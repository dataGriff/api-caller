package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// historyProject serves a counter whose body changes on every call and
// writes a project that keeps keep responses per request.
func historyProject(t *testing.T, keep int) string {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := n.Add(1)
		w.Header().Set("Content-Type", "application/json")
		status := http.StatusOK
		if r.URL.Path == "/flaky" && c%2 == 0 {
			status = http.StatusServiceUnavailable
		}
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"count":%d,"items":["a"],"stable":true}`, c)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), fmt.Sprintf(`### Count
# @name count
GET %[1]s/count
Authorization: Bearer secret-token

### Flaky
# @name flaky
GET %[1]s/flaky

### Unnamed
GET %[1]s/unnamed
`, srv.URL))
	if keep > 0 {
		mustWrite(t, filepath.Join(dir, "apic.yaml"), fmt.Sprintf("history: %d\n", keep))
	}
	return dir
}

func apic(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	app := New()
	var stdout, stderr bytes.Buffer
	app.Stdout, app.Stderr = &stdout, &stderr
	code := app.Execute(context.Background(), args)
	return stdout.String(), stderr.String(), code
}

func TestHistoryRecordsListsShowsAndDiffs(t *testing.T) {
	dir := historyProject(t, 2)
	for range 3 {
		if _, errOut, code := apic(t, "-C", dir, "run", "count"); code != 0 {
			t.Fatalf("run: %d %s", code, errOut)
		}
	}
	out, _, code := apic(t, "-C", dir, "history", "count")
	if code != 0 {
		t.Fatalf("history: %d", code)
	}
	if !strings.Contains(out, "count · default · 2 entries") || !strings.Contains(out, "#1") || !strings.Contains(out, "#2") || strings.Contains(out, "#3") {
		t.Errorf("history keeps the last 2:\n%s", out)
	}

	out, _, _ = apic(t, "-C", dir, "--json", "history", "count")
	var list struct {
		Env     string `json:"env"`
		Request string `json:"request"`
		Keep    int    `json:"keep"`
		Entries []struct {
			Index  int    `json:"index"`
			Status int    `json:"status"`
			File   string `json:"file"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if list.Env != "default" || list.Request != "count" || list.Keep != 2 || len(list.Entries) != 2 || list.Entries[0].Index != 1 || list.Entries[0].Status != 200 {
		t.Errorf("history --json = %+v", list)
	}

	out, _, _ = apic(t, "-C", dir, "history", "count", "--show", "1")
	if !strings.Contains(out, `"count": 3`) || !strings.Contains(out, "200 OK") {
		t.Errorf("--show 1 is the newest (count 3):\n%s", out)
	}
	stored, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(list.Entries[0].File)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "secret-token") {
		t.Error("the Authorization header was stored unmasked")
	}

	out, _, _ = apic(t, "-C", dir, "history", "diff", "count")
	if !strings.Contains(out, "~ $.count: 2 → 3") || !strings.Contains(out, "1 change") || strings.Contains(out, "stable") {
		t.Errorf("diff of the last two:\n%s", out)
	}
	out, _, _ = apic(t, "-C", dir, "--json", "history", "diff", "count", "1", "2")
	var diff struct {
		From    struct{ Index int } `json:"from"`
		To      struct{ Index int } `json:"to"`
		Changes []struct {
			Path, Op string
			From, To json.RawMessage
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(out), &diff); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if diff.From.Index != 1 || diff.To.Index != 2 || len(diff.Changes) != 1 || diff.Changes[0].Path != "$.count" || string(diff.Changes[0].From) != "3" || string(diff.Changes[0].To) != "2" {
		t.Errorf("diff --json 1 2 = %+v", diff)
	}

	out, _, _ = apic(t, "-C", dir, "history")
	if !strings.Contains(out, "count 2 entries") {
		t.Errorf("history with no request lists the requests:\n%s", out)
	}
}

func TestHistoryDiffShowsAStatusChange(t *testing.T) {
	dir := historyProject(t, 5)
	apic(t, "-C", dir, "run", "flaky")
	apic(t, "-C", dir, "run", "flaky")
	out, _, _ := apic(t, "-C", dir, "history", "diff", "flaky")
	if !strings.Contains(out, "~ status: 200 → 503") {
		t.Errorf("diff:\n%s", out)
	}
}

func TestHistoryIsOffUnlessConfigured(t *testing.T) {
	dir := historyProject(t, 0)
	apic(t, "-C", dir, "run", "count")
	if _, err := os.Stat(filepath.Join(dir, ".apic", "history")); err == nil {
		t.Error("history was recorded without history: in apic.yaml")
	}
	out, _, code := apic(t, "-C", dir, "history", "count")
	if code != 0 || !strings.Contains(out, "no history for count") || !strings.Contains(out, "history is off") {
		t.Errorf("history while off: %d\n%s", code, out)
	}
}

func TestHistoryIsNotRecordedForUnnamedNoSessionOrData(t *testing.T) {
	dir := historyProject(t, 5)
	apic(t, "-C", dir, "run", "api.http#3")
	apic(t, "-C", dir, "--no-session", "run", "count")
	mustWrite(t, filepath.Join(dir, "rows.csv"), "x\n1\n2\n")
	if _, errOut, code := apic(t, "-C", dir, "run", "count", "--data", filepath.Join(dir, "rows.csv")); code != 0 {
		t.Fatalf("data run: %d %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, ".apic", "history")); err == nil {
		t.Error("history recorded an unnamed request, a --no-session run or a data run")
	}
	_, errOut, code := apic(t, "-C", dir, "history", "api.http#3")
	if code != 2 || !strings.Contains(errOut, "no # @name") {
		t.Errorf("history of an unnamed request: %d %s", code, errOut)
	}
}

func TestHistoryRedactedRunsStoreTheRedactedForm(t *testing.T) {
	dir := historyProject(t, 5)
	apic(t, "-C", dir, "--redact", "run", "count")
	out, _, _ := apic(t, "-C", dir, "--json", "history", "count", "--show", "1")
	if strings.Contains(out, `"count"`+`: 1`) || !strings.Contains(out, `"body": "***"`) {
		t.Errorf("a --redact run stored its body:\n%s", out)
	}
}

func TestHistoryErrorsAndClear(t *testing.T) {
	dir := historyProject(t, 5)
	apic(t, "-C", dir, "run", "count")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"history", "diff", "count"}, "count has 1 entry in default; a diff needs two"},
		{[]string{"history", "count", "--show", "4"}, "count has 1 history entry, no #4"},
		{[]string{"history", "diff", "count", "0"}, `"0" is not an entry number`},
		{[]string{"history", "nope"}, `no request named "nope"`},
		{[]string{"history", "--show", "1"}, "--show needs a request"},
		{[]string{"history", "clear", "count", "--all"}, "give it no request"},
	} {
		_, errOut, code := apic(t, append([]string{"-C", dir}, c.args...)...)
		if code != 2 || !strings.Contains(errOut, c.want) {
			t.Errorf("%v: %d %s, want %q", c.args, code, errOut, c.want)
		}
	}

	apic(t, "-C", dir, "run", "flaky")
	out, _, _ := apic(t, "-C", dir, "--json", "history", "clear", "count")
	if !strings.Contains(out, `"request": "count"`) || !strings.Contains(out, `"entries": 1`) {
		t.Errorf("clear count:\n%s", out)
	}
	out, _, _ = apic(t, "-C", dir, "history")
	if strings.Contains(out, "count") || !strings.Contains(out, "flaky") {
		t.Errorf("clearing count should leave flaky:\n%s", out)
	}
	// A request that is gone from the files keeps its history until it
	// is cleared.
	mustWrite(t, filepath.Join(dir, "api.http"), "### Other\n# @name other\nGET http://example.com\n")
	out, _, code := apic(t, "-C", dir, "history", "flaky")
	if code != 0 || !strings.Contains(out, "flaky · default · 1 entry") {
		t.Errorf("history of a removed request: %d\n%s", code, out)
	}
	out, _, _ = apic(t, "-C", dir, "history", "clear")
	if !strings.Contains(out, "cleared 1 entry") {
		t.Errorf("clear the environment:\n%s", out)
	}
}
