package cli

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// retryProject serves a job that is running for two polls and done after.
func retryProject(t *testing.T) string {
	t.Helper()
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/job" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if polls.Add(1) >= 3 {
			_, _ = w.Write([]byte(`{"state":"done"}`))
			return
		}
		_, _ = w.Write([]byte(`{"state":"running"}`))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), `
### submit
# @name submit
# @assert status == 200
GET {{baseUrl}}/submit

### wait
# @name wait
# @retry 5 1ms
# @assert status == 200
# @assert body.$.state == done
GET {{baseUrl}}/job

### plain
# @name plain
# @assert body.$.state == done
GET {{baseUrl}}/job
`)
	mustWrite(t, filepath.Join(dir, "http-client.env.json"), `{"dev":{"baseUrl":"`+srv.URL+`"}}`)
	return dir
}

func TestRunPrintsAttemptsAsTheyHappen(t *testing.T) {
	dir := retryProject(t)
	code, out, errb := execute(t, "run", "submit", "wait", "-C", dir, "--env", "dev", "--no-session", "--no-color")
	if code != 0 {
		t.Fatalf("code=%d out=%s err=%s", code, out, errb)
	}
	// submit's report, then wait's attempt lines, then wait's report with
	// the attempt count.
	submit := strings.Index(out, "/submit")
	first := strings.Index(out, `attempt 1/5 · body.$.state == done: got "running"`)
	second := strings.Index(out, "attempt 2/5")
	report := strings.Index(out, "200 OK · ")
	report = strings.Index(out[report+1:], "200 OK · ") + report + 1
	if submit < 0 || first < 0 || second < 0 || submit > first || first > second || second > report {
		t.Fatalf("order wrong:\n%s", out)
	}
	if !strings.Contains(out, "· 3 attempts") || strings.Count(out, "attempt ") != 2 {
		t.Fatalf("the report should say 3 attempts after two attempt lines:\n%s", out)
	}
}

func TestRunJSONCarriesAttemptsAndNoProgress(t *testing.T) {
	dir := retryProject(t)
	code, out, errb := execute(t, "run", "wait", "-C", dir, "--env", "dev", "--no-session", "--json")
	if code != 0 || !strings.Contains(out, `"attempts":3`) || strings.Contains(out, "attempt 1/5") {
		t.Fatalf("code=%d out=%s err=%s", code, out, errb)
	}
}

func TestRetryFlags(t *testing.T) {
	dir := retryProject(t)
	// --retry applies to a request without its own policy.
	code, out, _ := execute(t, "run", "plain", "-C", dir, "--env", "dev", "--no-session", "--json", "--retry", "4 1ms")
	if code != 0 || !strings.Contains(out, `"attempts":3`) {
		t.Fatalf("--retry: code=%d out=%s", code, out)
	}
	// --no-retry sends once, and the assertion fails.
	dir = retryProject(t)
	code, out, _ = execute(t, "run", "wait", "-C", dir, "--env", "dev", "--no-session", "--json", "--no-retry")
	if code != 1 || strings.Contains(out, `"attempts"`) || !strings.Contains(out, `"ok":false`) {
		t.Fatalf("--no-retry: code=%d out=%s", code, out)
	}
	// A bad --retry is a usage error.
	code, _, errb := execute(t, "run", "plain", "-C", dir, "--env", "dev", "--no-session", "--retry", "soon")
	if code != 2 || !strings.Contains(errb, `--retry "soon"`) {
		t.Fatalf("bad --retry: code=%d err=%s", code, errb)
	}
}

func TestValidateReportsBadRetry(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "### a\n# @name a\n# @retry 0\nGET http://x\n")
	mustWrite(t, filepath.Join(dir, "apic.yaml"), "retry: 3 soon\n")
	code, out, _ := execute(t, "validate", "-C", dir, "--no-color")
	if code == 0 {
		t.Fatalf("validate should fail:\n%s", out)
	}
	for _, want := range []string{`api.http:3:10: error: @retry "0": attempts must be a whole number of at least 1, got "0" (bad-retry)`, `apic.yaml:0: error: retry "3 soon": interval must be a duration`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
