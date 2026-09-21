package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// refProject serves a login and a /me that needs its token, and writes a
// project where whoami declares `# @ref login`.
func refProject(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"t-1"}`))
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t-1" {
			http.Error(w, `{"error":"unauthorised"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"alice"}`))
	})
	mux.HandleFunc("GET /broken", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), `
### login
# @name login
# @assert status == 200
# @capture token = body.$.token
POST {{baseUrl}}/login

### whoami
# @name whoami
# @ref login
# @assert status == 200
GET {{baseUrl}}/me
Authorization: Bearer {{token}}

### broken
# @name broken
# @assert status == 200
# @capture other = body.$.token
GET {{baseUrl}}/broken

### needs-broken
# @name needs-broken
# @ref broken
GET {{baseUrl}}/me
Authorization: Bearer {{other}}
`)
	mustWrite(t, filepath.Join(dir, "http-client.env.json"), `{"dev":{"baseUrl":"`+srv.URL+`"}}`)
	return dir
}

func execute(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	a := New()
	var out, errb bytes.Buffer
	a.Stdout, a.Stderr = &out, &errb
	code := a.Execute(context.Background(), args)
	return code, out.String(), errb.String()
}

func TestRunPrintsWhatRefRanFirst(t *testing.T) {
	dir := refProject(t)
	code, out, errb := execute(t, "run", "whoami", "-C", dir, "--env", "dev", "--no-session", "--no-color")
	if code != 0 {
		t.Fatalf("code=%d out=%s err=%s", code, out, errb)
	}
	login := strings.Index(out, "POST ")
	note := strings.Index(out, "↳ ran login first (# @ref)")
	me := strings.Index(out, "GET ")
	if login < 0 || note < 0 || me < 0 || login > note || note > me {
		t.Fatalf("login's report, the note, then whoami's, got:\n%s", out)
	}
	if strings.Count(out, "200 OK") != 2 {
		t.Fatalf("both requests should report 200:\n%s", out)
	}
}

func TestRunJSONFlattensWhatRefRanFirst(t *testing.T) {
	dir := refProject(t)
	code, out, errb := execute(t, "run", "whoami", "-C", dir, "--env", "dev", "--no-session", "--json")
	if code != 0 {
		t.Fatalf("code=%d out=%s err=%s", code, out, errb)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one object per request sent, got %d:\n%s", len(lines), out)
	}
	var names []string
	for _, l := range lines {
		var obj struct {
			OK       bool                  `json:"ok"`
			Request  struct{ Name string } `json:"request"`
			RanFirst json.RawMessage       `json:"ran_first"`
		}
		if err := json.Unmarshal([]byte(l), &obj); err != nil {
			t.Fatalf("%v in %s", err, l)
		}
		if !obj.OK || obj.RanFirst != nil {
			t.Fatalf("each line is its own flat result: %s", l)
		}
		names = append(names, obj.Request.Name)
	}
	if strings.Join(names, ",") != "login,whoami" {
		t.Fatalf("order = %v", names)
	}
}

func TestRunFailsWhenARefFails(t *testing.T) {
	dir := refProject(t)
	code, out, _ := execute(t, "run", "needs-broken", "-C", dir, "--env", "dev", "--no-session", "--no-color")
	if code != 1 {
		t.Fatalf("a failed dependency is a failed run (exit 1), got %d:\n%s", code, out)
	}
	for _, want := range []string{"503", "↳ ran broken first (# @ref)", "✗ @ref broken failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "200 OK") {
		t.Fatalf("needs-broken should not have been sent:\n%s", out)
	}
}

func TestListAndDescribeReportRefs(t *testing.T) {
	dir := refProject(t)
	_, out, _ := execute(t, "list", "-C", dir, "--env", "dev", "--json")
	if !strings.Contains(out, `"refs": [`) || !strings.Contains(out, `"login"`) {
		t.Fatalf("list --json should carry refs:\n%s", out)
	}
	_, out, _ = execute(t, "describe", "whoami", "-C", dir, "--env", "dev", "--no-session", "--json")
	if !strings.Contains(out, `"refs": [`) || !strings.Contains(out, `"ref_runs": true`) || !strings.Contains(out, `"ready": true`) {
		t.Fatalf("describe --json should say login runs first and the request is ready:\n%s", out)
	}
	_, out, _ = execute(t, "describe", "whoami", "-C", dir, "--env", "dev", "--no-session", "--no-color")
	if !strings.Contains(out, "captured by login, which # @ref runs first") {
		t.Fatalf("describe should say login runs first:\n%s", out)
	}
}

func TestValidateReportsBadRefs(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "### a\n# @name a\n# @ref nope\n# @ref b\nGET http://x\n\n### b\n# @name b\n# @ref a\nGET http://x\n")
	code, out, _ := execute(t, "validate", "-C", dir, "--no-color")
	if code == 0 {
		t.Fatalf("validate should fail:\n%s", out)
	}
	for _, want := range []string{"api.http:3:8: error: @ref nope: no request named \"nope\"", "(bad-ref)", "api.http:4:8: error: @ref b is a cycle: a -> b -> a", "(ref-cycle)", "api.http:9:8: error: @ref a is a cycle: b -> a -> b"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRunVerboseShowsTimings(t *testing.T) {
	dir := refProject(t)
	code, out, errb := execute(t, "run", "login", "-C", dir, "--env", "dev", "--no-session", "--no-color", "-v")
	if code != 0 {
		t.Fatalf("code=%d out=%s err=%s", code, out, errb)
	}
	if !strings.Contains(out, "dns 0 ms · connect ") || !strings.Contains(out, " · new connection\n") {
		t.Fatalf("no timings line in verbose output:\n%s", out)
	}
	_, out, _ = execute(t, "run", "login", "-C", dir, "--env", "dev", "--no-session")
	if strings.Contains(out, "dns 0 ms") {
		t.Fatalf("timings should need -v:\n%s", out)
	}
	_, out, _ = execute(t, "run", "login", "-C", dir, "--env", "dev", "--no-session", "--json")
	if !strings.Contains(out, `"timings":{`) || !strings.Contains(out, `"reused":false`) {
		t.Fatalf("json should carry timings:\n%s", out)
	}
}
