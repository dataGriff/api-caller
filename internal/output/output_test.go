package output

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
	"github.com/dataGriff/api-caller/internal/session"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

func results(t *testing.T) []*runner.Result {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/42" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": 42, "name": "alice", "tags": ["a","b"], "active": true, "age": null}`))
			return
		}
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error": "nope"}`))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	src := "### Fetch\n# @name get-user\n# @assert status == 200\n# @assert body.$.name == alice\n# @capture uid = body.$.id\nGET {{baseUrl}}/users/42\nAccept: application/json\n\n### Missing\n# @name missing\n# @assert status == 200\nGET {{baseUrl}}/nope\n"
	if err := os.WriteFile(filepath.Join(dir, "api.http"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(p, runner.Options{Vars: map[string]string{"baseUrl": srv.URL}, Session: session.NewMemory(), KeepGoing: true})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.RunAll(context.Background(), p.Requests())
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHumanRendersStatusBodyChecksAndCaptures(t *testing.T) {
	res := results(t)
	var buf bytes.Buffer
	Human(&buf, res[0], false)
	got := buf.String()
	for _, want := range []string{"GET http://", "200 OK", " ms", "\"name\": \"alice\"", "✓ status == 200", "✓ body.$.name == alice", "↳ uid = 42"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("ascii profile should produce no escapes:\n%q", got)
	}
	buf.Reset()
	Human(&buf, res[0], true)
	if !strings.Contains(buf.String(), "Accept: application/json") || !strings.Contains(buf.String(), "content-type: application/json") {
		t.Errorf("verbose output should include request and response headers:\n%s", buf.String())
	}
}

func TestFailureShowsActualAndSummaryTable(t *testing.T) {
	res := results(t)
	var buf bytes.Buffer
	Human(&buf, res[1], false)
	if !strings.Contains(buf.String(), "✗ status == 200 (actual: 404)") {
		t.Errorf("failed assertion should show the actual value:\n%s", buf.String())
	}
	if checks := Checks(Default(), res[1], 80, true); !strings.Contains(checks, "expected: 200") {
		t.Errorf("Checks with expected should show the expected value:\n%s", checks)
	}
	buf.Reset()
	Summary(&buf, res)
	got := buf.String()
	for _, want := range []string{"✓ get-user", "✗ missing", "404", "status == 200 (actual: 404)", "1 failed, 1 passed", "2 requests"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestHighlightJSON(t *testing.T) {
	in := "{\n  \"key\": \"a:b\",\n  \"n\": -1.5e3,\n  \"ok\": true,\n  \"nil\": null\n}"
	if got := HighlightJSON(Default(), in); got != in {
		t.Fatalf("ascii profile must not change the text:\n%s", got)
	}
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	th := Default()
	got := HighlightJSON(th, in)
	if !strings.Contains(got, th.Key.Render(`"key"`)) {
		t.Errorf("key not styled as key: %q", got)
	}
	if !strings.Contains(got, th.Str.Render(`"a:b"`)) {
		t.Errorf("string value with a colon must be styled as a string: %q", got)
	}
	if !strings.Contains(got, th.Num.Render("-1.5e3")) || !strings.Contains(got, th.Lit.Render("true")) || !strings.Contains(got, th.Lit.Render("null")) {
		t.Errorf("numbers and literals not styled: %q", got)
	}
}

func TestTruncateIsRuneSafe(t *testing.T) {
	if got := Truncate("héllo wörld", 5); got != "héllo…" {
		t.Fatalf("got %q", got)
	}
	if got := Truncate("a\nb", 10); got != "a b" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderBodyLeavesNonJSONAlone(t *testing.T) {
	if got := RenderBody(Default(), []byte("<html>hi</html>")); got != "<html>hi</html>" {
		t.Fatalf("got %q", got)
	}
}
