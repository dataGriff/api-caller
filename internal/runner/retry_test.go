package runner

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dataGriff/api-caller/internal/session"
)

// jobServer answers "running" for the first two polls and "done" after, and
// counts the polls.
func jobServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /job", func(w http.ResponseWriter, _ *http.Request) {
		n := polls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		state := "running"
		if n >= 3 {
			state = "done"
		}
		_, _ = w.Write([]byte(`{"state":"` + state + `","poll":` + itoa(int(n)) + `}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &polls
}

const retryHTTP = `
### Wait for the job
# @name wait
# @retry 5 250ms
# @assert status == 200
# @assert body.$.state == done
# @capture poll = body.$.poll
GET {{baseUrl}}/job

### Not enough attempts
# @name short
# @retry 2 10ms
# @assert body.$.state == done
GET {{baseUrl}}/job

### No policy of its own
# @name plain
# @assert body.$.state == done
GET {{baseUrl}}/job

### Bad policy
# @name bad
# @retry soon
GET {{baseUrl}}/job
`

type fakeSleeper struct {
	waits []time.Duration
	err   error
}

func (f *fakeSleeper) sleep(_ context.Context, d time.Duration) error {
	f.waits = append(f.waits, d)
	return f.err
}

func retryRunner(t *testing.T, opts Options, files map[string]string) (*Runner, *atomic.Int32, *fakeSleeper) {
	t.Helper()
	srv, polls := jobServer(t)
	all := map[string]string{
		"api.http":             retryHTTP,
		"http-client.env.json": `{"dev": {"baseUrl": "` + srv.URL + `"}}`,
	}
	for k, v := range files {
		all[k] = v
	}
	dir := writeProject(t, all)
	opts.Env = "dev"
	if opts.Session == nil {
		opts.Session = session.NewMemory()
	}
	r := newRunner(t, dir, opts)
	f := &fakeSleeper{}
	r.sleep = f.sleep
	return r, polls, f
}

func TestRetryResendsUntilTheAssertionsPass(t *testing.T) {
	r, polls, sleeper := retryRunner(t, Options{}, nil)
	var progress []Progress
	r.Progress = func(p Progress) { progress = append(progress, p) }
	res, err := r.Run(context.Background(), lookup(t, r, "wait"))
	if err != nil || !res.OK {
		t.Fatalf("%+v err=%v", res, err)
	}
	if res.Attempts != 3 || polls.Load() != 3 {
		t.Fatalf("attempts=%d polls=%d", res.Attempts, polls.Load())
	}
	if len(sleeper.waits) != 2 || sleeper.waits[0] != 250*time.Millisecond {
		t.Fatalf("waits = %v", sleeper.waits)
	}
	if len(progress) != 2 || progress[0].Attempt != 1 || progress[0].Max != 5 || progress[1].Attempt != 2 {
		t.Fatalf("progress = %+v", progress)
	}
	if want := `body.$.state == done: got "running"`; progress[0].Failure != want {
		t.Fatalf("failure = %q, want %q", progress[0].Failure, want)
	}
	// Captures come from the attempt that passed, and only that one is
	// committed to the session.
	if res.Captures["poll"] != "3" {
		t.Fatalf("captures = %v", res.Captures)
	}
	if v, ok := r.Session.Get("dev", "poll"); !ok || v != "3" {
		t.Fatalf("session poll = %q, %v", v, ok)
	}
	// The passing response is what the result shows.
	if !strings.Contains(string(res.DisplayRawBody()), `"done"`) {
		t.Fatalf("body = %s", res.DisplayRawBody())
	}
}

func TestRetryExhaustedReportsTheLastAttempt(t *testing.T) {
	r, polls, _ := retryRunner(t, Options{}, nil)
	res, err := r.Run(context.Background(), lookup(t, r, "short"))
	if err != nil {
		t.Fatalf("exhausting the attempts is an assertion failure, not an error: %v", err)
	}
	if res.OK || res.Attempts != 2 || polls.Load() != 2 {
		t.Fatalf("ok=%v attempts=%d polls=%d", res.OK, res.Attempts, polls.Load())
	}
	if len(res.Asserts) != 1 || res.Asserts[0].Pass || res.Asserts[0].Actual != "running" {
		t.Fatalf("asserts = %+v", res.Asserts)
	}
}

func TestRetryPolicyPrecedence(t *testing.T) {
	ctx := context.Background()
	// No directive, no flag, no config: one attempt, no attempts count.
	r, polls, _ := retryRunner(t, Options{}, nil)
	res, err := r.Run(ctx, lookup(t, r, "plain"))
	if err != nil || res.OK || res.Attempts != 0 || polls.Load() != 1 {
		t.Fatalf("plain: ok=%v attempts=%d polls=%d err=%v", res.OK, res.Attempts, polls.Load(), err)
	}
	// apic.yaml supplies a default.
	r, polls, sleeper := retryRunner(t, Options{}, map[string]string{"apic.yaml": "retry: 4 100ms\n"})
	res, err = r.Run(ctx, lookup(t, r, "plain"))
	if err != nil || !res.OK || res.Attempts != 3 || polls.Load() != 3 || sleeper.waits[0] != 100*time.Millisecond {
		t.Fatalf("config: ok=%v attempts=%d polls=%d waits=%v err=%v", res.OK, res.Attempts, polls.Load(), sleeper.waits, err)
	}
	// --retry beats apic.yaml; the directive beats both.
	r, polls, sleeper = retryRunner(t, Options{Retry: "3"}, map[string]string{"apic.yaml": "retry: 1\n"})
	res, err = r.Run(ctx, lookup(t, r, "plain"))
	if err != nil || !res.OK || res.Attempts != 3 || sleeper.waits[0] != time.Second {
		t.Fatalf("flag: ok=%v attempts=%d polls=%d waits=%v err=%v", res.OK, res.Attempts, polls.Load(), sleeper.waits, err)
	}
	r, _, _ = retryRunner(t, Options{Retry: "9"}, nil)
	res, err = r.Run(ctx, lookup(t, r, "short"))
	if err != nil || res.OK || res.Attempts != 2 {
		t.Fatalf("directive beats flag: ok=%v attempts=%d err=%v", res.OK, res.Attempts, err)
	}
	// --no-retry switches every policy off.
	r, polls, _ = retryRunner(t, Options{NoRetry: true, Retry: "9"}, map[string]string{"apic.yaml": "retry: 9\n"})
	res, err = r.Run(ctx, lookup(t, r, "wait"))
	if err != nil || res.OK || res.Attempts != 0 || polls.Load() != 1 {
		t.Fatalf("no-retry: ok=%v attempts=%d polls=%d err=%v", res.OK, res.Attempts, polls.Load(), err)
	}
	// Bad policies are usage errors that name their source.
	r, _, _ = retryRunner(t, Options{Retry: "lots"}, nil)
	for name, want := range map[string]string{"bad": `bad @retry "soon"`, "plain": `--retry "lots"`} {
		_, err := r.Run(ctx, lookup(t, r, name))
		var ue *UsageError
		if !errors.As(err, &ue) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want usage error with %q, got %v", name, want, err)
		}
	}
	r, _, _ = retryRunner(t, Options{}, map[string]string{"apic.yaml": "retry: never\n"})
	if _, err := r.Run(ctx, lookup(t, r, "plain")); err == nil || !strings.Contains(err.Error(), `apic.yaml: bad retry "never"`) {
		t.Errorf("config: got %v", err)
	}
}

func TestRetryCoversTransportErrorsAndStopsOnCancel(t *testing.T) {
	// A server that is not listening: every attempt is a transport error.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	dir := writeProject(t, map[string]string{
		"api.http":             "### down\n# @name down\n# @retry 3 10ms\nGET http://" + addr + "/\n",
		"http-client.env.json": `{"dev": {}}`,
	})
	r := newRunner(t, dir, Options{Env: "dev", Session: session.NewMemory()})
	sleeper := &fakeSleeper{}
	r.sleep = sleeper.sleep
	var attempts int
	r.Progress = func(Progress) { attempts++ }
	_, err = r.Run(context.Background(), lookup(t, r, "down"))
	var te *TransportError
	if !errors.As(err, &te) || attempts != 2 || len(sleeper.waits) != 2 {
		t.Fatalf("want a transport error after 3 attempts, got %v (progress %d, waits %v)", err, attempts, sleeper.waits)
	}
	// Cancelled while waiting between attempts: a transport error, at once.
	sleeper.err, sleeper.waits = context.Canceled, nil
	_, err = r.Run(context.Background(), lookup(t, r, "down"))
	if !errors.As(err, &te) || !errors.Is(err, context.Canceled) || len(sleeper.waits) != 1 {
		t.Fatalf("want cancellation surfaced as a transport error after one wait, got %v (waits %v)", err, sleeper.waits)
	}
}

func TestSleepCtxHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Fatalf("zero wait: %v", err)
	}
	start := time.Now()
	if err := sleepCtx(context.Background(), 5*time.Millisecond); err != nil || time.Since(start) < 5*time.Millisecond {
		t.Fatalf("short wait: %v after %s", err, time.Since(start))
	}
}

func TestRetryRunsEachAttemptWithItsOwnTimeout(t *testing.T) {
	// The first attempt hangs past the timeout; the second answers at once.
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		_, _ = w.Write([]byte(`ok`))
	}))
	t.Cleanup(srv.Close)
	dir := writeProject(t, map[string]string{
		"api.http":             "### slow\n# @name slow\n# @retry 2 0s\n# @timeout 100ms\n# @assert status == 200\nGET " + srv.URL + "/\n",
		"http-client.env.json": `{"dev": {}}`,
	})
	if err := os.WriteFile(filepath.Join(dir, ".keep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := newRunner(t, dir, Options{Env: "dev", Session: session.NewMemory()})
	res, err := r.Run(context.Background(), lookup(t, r, "slow"))
	if err != nil || !res.OK || res.Attempts != 2 {
		t.Fatalf("%+v err=%v", res, err)
	}
}
