package cli

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// A file run as a flow skips a # @disabled request and counts it; naming
// it sends it.
func TestRunSkipsDisabledInAFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "### one\n# @name one\nGET {{baseUrl}}/one\n\n### off\n# @name off\n# @disabled\nGET {{baseUrl}}/off\n")
	mustWrite(t, filepath.Join(dir, "http-client.env.json"), `{"dev":{"baseUrl":"`+srv.URL+`"}}`)
	args := []string{"-C", dir, "--env", "dev", "--no-session", "--no-color"}
	code, out, errb := execute(t, append([]string{"run", "api.http"}, args...)...)
	if code != 0 || !strings.Contains(out, "skipped (disabled)") || !strings.Contains(out, "1 passed, 1 skipped") {
		t.Fatalf("code=%d out=%s err=%s", code, out, errb)
	}
	code, out, _ = execute(t, append([]string{"run", "api.http", "--json"}, args...)...)
	if code != 0 || !strings.Contains(out, `"skipped":"disabled"`) {
		t.Fatalf("json: code=%d out=%s", code, out)
	}
	code, out, _ = execute(t, append([]string{"run", "off"}, args...)...)
	if code != 0 || !strings.Contains(out, "200 OK") || strings.Contains(out, "skipped") {
		t.Fatalf("by name: code=%d out=%s", code, out)
	}
	code, out, _ = execute(t, append([]string{"list", "--json"}, args[:2]...)...)
	if code != 0 || strings.Count(out, `"disabled": true`) != 1 {
		t.Fatalf("list: code=%d out=%s", code, out)
	}
}
