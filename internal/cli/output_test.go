package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `apic run --output` saves one request's body from the working directory;
// a flow is refused, and so is a project input as the target.
func TestRunOutputFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte("date,total\n2026-01-01,3\n"))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "### Report\n# @name report\nGET {{baseUrl}}/report\n\n### Other\n# @name other\nGET {{baseUrl}}/report\n")
	mustWrite(t, filepath.Join(dir, "http-client.env.json"), `{"dev":{"baseUrl":"`+srv.URL+`"}}`)
	out := filepath.Join(t.TempDir(), "daily.csv")
	code, stdout, stderr := execute(t, "run", "report", "-C", dir, "--env", "dev", "--no-session", "--no-color", "--output", out)
	if code != 0 || !strings.Contains(stdout, "saved to "+out) {
		t.Fatalf("code=%d out=%s err=%s", code, stdout, stderr)
	}
	if got, err := os.ReadFile(out); err != nil || string(got) != "date,total\n2026-01-01,3\n" {
		t.Fatalf("saved %q %v", got, err)
	}
	code, _, stderr = execute(t, "run", "report", "other", "-C", dir, "--env", "dev", "--no-session", "--output", out)
	if code != 2 || !strings.Contains(stderr, "--output saves one response") {
		t.Fatalf("flow: code=%d err=%s", code, stderr)
	}
	code, _, stderr = execute(t, "run", "report", "-C", dir, "--env", "dev", "--no-session", "--output", filepath.Join(dir, "api.http"))
	if code != 2 || !strings.Contains(stderr, "would overwrite a project file") {
		t.Fatalf("input: code=%d err=%s", code, stderr)
	}
	code, stdout, stderr = execute(t, "run", "report", "-C", dir, "--env", "dev", "--no-session", "--json", "--output", out)
	if code != 0 || !strings.Contains(stdout, `"saved_to":`) {
		t.Fatalf("json: code=%d out=%s err=%s", code, stdout, stderr)
	}
}
