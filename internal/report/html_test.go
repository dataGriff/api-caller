package report

import (
	"bytes"
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// golden compares got with testdata/name, rewriting it under -update.
// Both sides are read with CRLF folded to LF, so the comparison is the
// same whatever line endings a checkout or a fixture carries. On a
// mismatch the output is left beside the golden as name.got; a stale
// .got from an earlier failure is removed when the test passes.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil { //nolint:gosec // test fixture
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	gotPath := path + ".got"
	if bytes.Equal(want, got) {
		_ = os.Remove(gotPath)
		return
	}
	where := "the output is in " + gotPath
	if err := os.WriteFile(gotPath, got, 0o644); err != nil { //nolint:gosec // test output
		where = "the output could not be written beside it: " + err.Error()
	}
	t.Fatalf("%s differs from the golden file; %s (run with -update to accept it)", name, where)
}

func apiServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "sid=abc")
		_, _ = w.Write([]byte(`{"token":"t-1","user":{"name":"alice"}}`))
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t-1" {
			http.Error(w, `{"error":"unauthorised"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"alice","roles":["admin"]}`))
	})
	mux.HandleFunc("GET /missing", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nothing here", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func results(t *testing.T, redact bool) ([]*runner.Result, string) {
	t.Helper()
	srv := apiServer(t)
	dir := t.TempDir()
	files := map[string]string{
		"http-client.env.json":         `{"dev": {"baseUrl": "` + srv.URL + `"}}`,
		"http-client.private.env.json": `{"dev": {"password": "s3cret"}}`,
		"api.http": `
### Log in
# @name login
# @assert status == 200
# @capture token = body.$.token
POST {{baseUrl}}/login
Content-Type: application/json

{"user": "alice", "password": "{{password}}"}

### Who am I
# @name me
# @ref login
# @assert status == 200
# @assert body.$.name == alice
GET {{baseUrl}}/me
Authorization: Bearer {{token}}

### Not there
# @name missing
# @assert status == 200
# @assert header.content-type contains json
GET {{baseUrl}}/missing
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(p, runner.Options{Env: "dev", NoSession: true, Redact: redact, KeepGoing: true})
	if err != nil {
		t.Fatal(err)
	}
	var reqs []*runner.Result
	for _, name := range []string{"me", "missing"} {
		req, err := p.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		res, err := r.Run(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		reqs = append(reqs, res)
	}
	return reqs, srv.URL
}

// normalise strips what varies between runs: the server port and the
// measured times.
func normalise(html, serverURL string) []byte {
	s := strings.ReplaceAll(html, serverURL, "http://api.test")
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, `class="timings"`) {
			line = `<div class="timings">(timings)</div>`
		}
		line = dateRe.ReplaceAllString(line, "<th>date</th><td>(date)</td>")
		out = append(out, line)
	}
	s = strings.Join(out, "\n")
	// "<n> ms" durations are measured; pin them.
	return []byte(msRe.ReplaceAllString(s, "0 ms"))
}

var (
	msRe   = regexp.MustCompile(`\b\d+ ms\b`)
	dateRe = regexp.MustCompile(`<th>date</th><td>[^<]*</td>`)
)

func TestRunReportGolden(t *testing.T) {
	res, url := results(t, false)
	var buf bytes.Buffer
	meta := Meta{Version: "test", Env: "dev", Time: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC), Project: "/tmp/project"}
	if err := Run(&buf, meta, res); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{"<!doctype html>", "1 failed", "ran first (# @ref)", "status == 200", "header.content-type contains json", "token = t-1", `&#34;name&#34;: &#34;alice&#34;`, "Authorization", "***", "nothing here", "environment <b>dev</b>"} {
		if !strings.Contains(html, want) {
			t.Errorf("report lacks %q", want)
		}
	}
	// Bodies are shown in full without --redact, as run -v shows them;
	// the response's Set-Cookie is masked either way.
	if strings.Contains(html, "sid=abc") {
		t.Error("set-cookie is shown although sensitive")
	}
	golden(t, "run.golden.html", normalise(html, url))
}

func TestRunReportRedacted(t *testing.T) {
	res, url := results(t, true)
	var buf bytes.Buffer
	if err := Run(&buf, Meta{Version: "test", Env: "dev", Redacted: true, Time: time.Unix(0, 0)}, res); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, secret := range []string{"t-1", "alice", "s3cret", "admin", "nothing here"} {
		if strings.Contains(html, secret) {
			t.Errorf("redacted report contains %q", secret)
		}
	}
	for _, want := range []string{`class="badge redacted">redacted`, "1 failed", "status == ***", "token = ***"} {
		if !strings.Contains(html, want) {
			t.Errorf("redacted report lacks %q", want)
		}
	}
	golden(t, "run-redacted.golden.html", normalise(html, url))
}

const cucumberFixture = `[{"uri":"features/users.feature","keyword":"Feature","name":"Users","elements":[
 {"keyword":"Background","name":"","type":"background","steps":[{"keyword":"Given ","name":"I am logged in","result":{"status":"passed","duration":1200000}}]},
 {"keyword":"Scenario","name":"Fetch a user","type":"scenario","steps":[
   {"keyword":"When ","name":"I fetch user 42","result":{"status":"passed","duration":800000}},
   {"keyword":"Then ","name":"the response status is 200","result":{"status":"passed","duration":10000}},
   {"keyword":"And ","name":"the response body matches:","doc_string":{"value":"{\"id\": 42}"},"result":{"status":"passed","duration":10000}}]},
 {"keyword":"Background","name":"","type":"background","steps":[{"keyword":"Given ","name":"I am logged in","result":{"status":"passed","duration":1000000}}]},
 {"keyword":"Scenario","name":"A missing user","type":"scenario","steps":[
   {"keyword":"When ","name":"I fetch user 999","result":{"status":"passed","duration":700000}},
   {"keyword":"Then ","name":"the response status is 200","result":{"status":"failed","duration":5000,"error_message":"expected status == 200, got \"404\"\n  GET http://api.test/users/999"}},
   {"keyword":"And ","name":"the variables:","rows":[{"cells":["name","value"]},{"cells":["a","1"]}],"result":{"status":"skipped"}}]},
 {"keyword":"Scenario","name":"Not written yet","type":"scenario","steps":[
   {"keyword":"When ","name":"I do something new","result":{"status":"undefined"}}]}
]}]`

func TestFeaturesReportGolden(t *testing.T) {
	var buf bytes.Buffer
	meta := Meta{Version: "test", Env: "dev", Time: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}
	if err := Features(&buf, meta, []byte(cucumberFixture)); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{"2 failed", "3 scenarios · 9 steps", "Fetch a user", "expected status == 200", "undefined steps", `<td>name</td>`, `&#34;id&#34;: 42`, "I am logged in"} {
		if !strings.Contains(html, want) {
			t.Errorf("report lacks %q", want)
		}
	}
	golden(t, "features.golden.html", []byte(html))
	// An empty run is a report too.
	buf.Reset()
	if err := Features(&buf, meta, nil); err != nil || !strings.Contains(buf.String(), "all passed") {
		t.Fatalf("empty: %v %s", err, buf.String())
	}
	if err := Features(&buf, meta, []byte("nonsense")); err == nil {
		t.Fatal("bad JSON should be an error")
	}
}
