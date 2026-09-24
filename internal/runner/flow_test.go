package runner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const flowHTTP = `
### One
# @name one
GET {{baseUrl}}/one

### Kept out of the flow
# @name off
# @disabled
GET {{baseUrl}}/off

### Rate limited
# @name slow
# @sleep 1500ms
GET {{baseUrl}}/slow

### Bad sleep
# @name bad-sleep
# @sleep soon
GET {{baseUrl}}/slow
`

func flowRunner(t *testing.T) (*Runner, map[string]*atomic.Int32, *fakeSleeper) {
	t.Helper()
	hits := map[string]*atomic.Int32{"/one": {}, "/off": {}, "/slow": {}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := hits[r.URL.Path]; n != nil {
			n.Add(1)
		}
	}))
	t.Cleanup(srv.Close)
	dir := writeProject(t, map[string]string{
		"api.http":             flowHTTP,
		"http-client.env.json": `{"dev": {"baseUrl": "` + srv.URL + `"}}`,
	})
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true, KeepGoing: true})
	f := &fakeSleeper{}
	r.sleep = f.sleep
	return r, hits, f
}

// A file run as a flow skips a # @disabled request and says so; naming it
// sends it.
func TestDisabledIsSkippedByFlows(t *testing.T) {
	r, hits, _ := flowRunner(t)
	ctx := context.Background()
	reqs, err := r.Target("api.http")
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	r.OnResult = func(res *Result, _ error) { seen = append(seen, res.Request.Name+":"+res.Skipped) }
	results, err := r.RunAll(ctx, reqs)
	if err == nil || !strings.Contains(err.Error(), "@sleep") {
		t.Fatalf("the bad @sleep is a usage error, got %v", err)
	}
	if len(results) != 4 || results[1].Skipped != "disabled" || !results[1].OK || results[1].Response != nil {
		t.Fatalf("results: %+v", results)
	}
	if hits["/off"].Load() != 0 || hits["/one"].Load() != 1 {
		t.Fatalf("hits: one=%d off=%d", hits["/one"].Load(), hits["/off"].Load())
	}
	if strings.Join(seen, ",") != "one:,off:disabled,slow:,bad-sleep:" {
		t.Fatalf("OnResult saw %v", seen)
	}

	// By name, or file#name, it goes out.
	for _, target := range []string{"off", "api.http#off", "api.http#2"} {
		reqs, err := r.Target(target)
		if err != nil {
			t.Fatal(err)
		}
		results, err := r.RunAll(ctx, reqs)
		if err != nil || len(results) != 1 || results[0].Skipped != "" || results[0].Response == nil {
			t.Fatalf("%s: %+v %v", target, results, err)
		}
	}
	if hits["/off"].Load() != 3 {
		t.Fatalf("off sent %d times, want 3", hits["/off"].Load())
	}
}

// # @sleep waits before sending, through the runner's sleeper, and a
// cancelled wait sends nothing.
func TestSleepBeforeSending(t *testing.T) {
	r, hits, sleeper := flowRunner(t)
	req, err := r.Project.Lookup("slow")
	if err != nil {
		t.Fatal(err)
	}
	if res, err := r.Run(context.Background(), req); err != nil || !res.OK {
		t.Fatalf("slow: %+v %v", res, err)
	}
	if len(sleeper.waits) != 1 || sleeper.waits[0] != 1500*time.Millisecond || hits["/slow"].Load() != 1 {
		t.Fatalf("waits %v, hits %d", sleeper.waits, hits["/slow"].Load())
	}
	if d := r.Describe(req); d.Sleep != "1500ms" || d.Disabled {
		t.Fatalf("describe: sleep %q disabled %v", d.Sleep, d.Disabled)
	}
	sleeper.err = context.Canceled
	_, err = r.Run(context.Background(), req)
	var te *TransportError
	if !errors.As(err, &te) || !errors.Is(err, context.Canceled) || hits["/slow"].Load() != 1 {
		t.Fatalf("a cancelled sleep: %v, hits %d", err, hits["/slow"].Load())
	}
	if off, _ := r.Project.Lookup("off"); !r.Describe(off).Disabled {
		t.Fatal("describe does not say off is disabled")
	}
	var codes []string
	for _, d := range r.Project.Validate() {
		codes = append(codes, fmt.Sprintf("%s@%d:%d", d.Code, d.Line, d.Column))
	}
	if strings.Join(codes, ",") != "bad-sleep@18:10" {
		t.Fatalf("validate: %v", codes)
	}
}
