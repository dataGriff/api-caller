package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/session"
)

// refServer counts logins so the tests can tell when `# @ref` ran one.
func refServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var logins atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, _ *http.Request) {
		logins.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-` + itoa(int(logins.Load())) + `"}`))
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-") {
			http.Error(w, `{"error":"unauthorised"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"alice@example.com","token":"` + strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") + `"}`))
	})
	mux.HandleFunc("GET /region", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"region":"eu"}`))
	})
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"nope"}`, http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &logins
}

func itoa(n int) string { return string(rune('0' + n)) }

const refHTTP = `
### Log in
# @name login
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login

### Who am I
# @name whoami
# @ref login
# @assert status == 200
GET {{baseUrl}}/me
Authorization: Bearer {{token}}

### Always fresh
# @name whoami-fresh
# @forceRef login
# @assert status == 200
# @capture used = body.$.token
GET {{baseUrl}}/me
Authorization: Bearer {{token}}

### Region, then who am I
# @name region
# @capture region = body.$.region
GET {{baseUrl}}/region

### Two levels down
# @name whoami-in-region
# @ref whoami
# @ref region
# @assert status == 200
GET {{baseUrl}}/me?region={{region}}
Authorization: Bearer {{token}}

### A diamond: both paths lead to login, which must run once
# @name diamond
# @ref whoami
# @ref whoami-in-region
# @assert status == 200
GET {{baseUrl}}/me?region={{region}}
Authorization: Bearer {{token}}

### Depends on a failing request
# @name broken-login
# @assert status == 200
# @capture token = body.$.access_token
GET {{baseUrl}}/fail

### Needs the broken one
# @name after-broken
# @ref broken-login
GET {{baseUrl}}/me
Authorization: Bearer {{token}}

### No such ref
# @name dangling
# @ref nope
GET {{baseUrl}}/me
Authorization: Bearer {{token}}

### Cycle a
# @name cycle-a
# @ref cycle-b
GET {{baseUrl}}/me
Authorization: Bearer {{fromB}}

### Cycle b
# @name cycle-b
# @ref cycle-a
GET {{baseUrl}}/me
Authorization: Bearer {{fromA}}

### Refers to itself
# @name selfish
# @ref selfish
GET {{baseUrl}}/me
Authorization: Bearer {{fromSelf}}
`

func refProject(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	srv, logins := refServer(t)
	dir := writeProject(t, map[string]string{
		"api.http":             refHTTP,
		"http-client.env.json": `{"dev": {"baseUrl": "` + srv.URL + `"}}`,
	})
	return dir, logins
}

func lookup(t *testing.T, r *Runner, name string) *httpfile.Request {
	t.Helper()
	req, err := r.Project.Lookup(name)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestRefRunsTheDependencyWhenAVariableIsMissing(t *testing.T) {
	dir, logins := refProject(t)
	ctx := context.Background()
	r := newRunner(t, dir, Options{Env: "dev", Session: session.NewMemory()})

	res, err := r.Run(ctx, lookup(t, r, "whoami"))
	if err != nil || !res.OK {
		t.Fatalf("whoami: %+v err=%v", res, err)
	}
	if logins.Load() != 1 {
		t.Fatalf("login ran %d times, want 1", logins.Load())
	}
	if len(res.Deps) != 1 || res.Deps[0].Request.Name != "login" || !res.Deps[0].OK || res.Deps[0].Req() == nil || res.Deps[0].Req().Name != "login" {
		t.Fatalf("deps = %+v", res.Deps)
	}
	if res.Req() == nil || res.Req().Name != "whoami" {
		t.Fatalf("result should know its request, got %v", res.Req())
	}
	// The dependency's result is in the JSON under ran_first.
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		RanFirst []struct {
			OK      bool `json:"ok"`
			Request struct {
				Name string `json:"name"`
			} `json:"request"`
		} `json:"ran_first"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.RanFirst) != 1 || out.RanFirst[0].Request.Name != "login" || !out.RanFirst[0].OK {
		t.Fatalf("ran_first = %s", data)
	}

	// The token is captured now: a second run does not log in again, and
	// reports no dependency.
	res, err = r.Run(ctx, lookup(t, r, "whoami"))
	if err != nil || !res.OK || len(res.Deps) != 0 || logins.Load() != 1 {
		t.Fatalf("second whoami: deps=%v logins=%d err=%v", res.Deps, logins.Load(), err)
	}

	// A fresh runner sharing the session finds the token and skips login.
	r2 := newRunner(t, dir, Options{Env: "dev", Session: r.Session})
	res, err = r2.Run(ctx, lookup(t, r2, "whoami"))
	if err != nil || !res.OK || len(res.Deps) != 0 || logins.Load() != 1 {
		t.Fatalf("whoami from session: deps=%v logins=%d err=%v", res.Deps, logins.Load(), err)
	}

	// Once the value is gone again (the UI keeps one runner across runs and
	// can clear the session), the ref runs again: "once" is per invocation.
	r.Session.Clear("dev")
	r.captured = map[string]string{}
	res, err = r.Run(ctx, lookup(t, r, "whoami"))
	if err != nil || !res.OK || len(res.Deps) != 1 || logins.Load() != 2 {
		t.Fatalf("whoami after clearing: deps=%v logins=%d err=%v", names(res.Deps), logins.Load(), err)
	}
}

func TestForceRefRunsEveryTime(t *testing.T) {
	dir, logins := refProject(t)
	ctx := context.Background()
	r := newRunner(t, dir, Options{Env: "dev", Session: session.NewMemory()})
	for i := 1; i <= 2; i++ {
		res, err := r.Run(ctx, lookup(t, r, "whoami-fresh"))
		if err != nil || !res.OK || len(res.Deps) != 1 {
			t.Fatalf("run %d: %+v err=%v", i, res, err)
		}
		if want := "tok-" + itoa(i); res.Captures["used"] != want {
			t.Fatalf("run %d used %q, want the token of login %d", i, res.Captures["used"], i)
		}
	}
	if logins.Load() != 2 {
		t.Fatalf("login ran %d times, want 2", logins.Load())
	}
}

func TestRefRunsOncePerInvocationAndNests(t *testing.T) {
	dir, logins := refProject(t)
	ctx := context.Background()
	r := newRunner(t, dir, Options{Env: "dev", Session: session.NewMemory()})
	res, err := r.Run(ctx, lookup(t, r, "whoami-in-region"))
	if err != nil || !res.OK {
		t.Fatalf("%+v err=%v", res, err)
	}
	// whoami ran first (pulling login in under it), then region.
	if len(res.Deps) != 2 || res.Deps[0].Request.Name != "whoami" || res.Deps[1].Request.Name != "region" {
		t.Fatalf("deps = %+v", names(res.Deps))
	}
	if d := res.Deps[0].Deps; len(d) != 1 || d[0].Request.Name != "login" {
		t.Fatalf("nested deps = %+v", names(d))
	}
	if logins.Load() != 1 {
		t.Fatalf("login ran %d times", logins.Load())
	}
	if !strings.Contains(res.Request.URL, "region=eu") {
		t.Fatalf("region capture not used: %s", res.Request.URL)
	}
	// Once run, a ref is not run again by this runner even when a later
	// request is missing something else.
	res, err = r.Run(ctx, lookup(t, r, "whoami-in-region"))
	if err != nil || !res.OK || len(res.Deps) != 0 {
		t.Fatalf("second run: deps=%v err=%v", names(res.Deps), err)
	}
}

func names(rs []*Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Request.Name)
	}
	return out
}

func TestRefFailureStopsTheRequest(t *testing.T) {
	dir, _ := refProject(t)
	ctx := context.Background()
	r := newRunner(t, dir, Options{Env: "dev", Session: session.NewMemory()})
	res, err := r.Run(ctx, lookup(t, r, "after-broken"))
	if err != nil {
		t.Fatalf("an assertion failure in a dependency is not an error: %v", err)
	}
	if res.OK || res.Response != nil || len(res.Deps) != 1 || res.Deps[0].OK {
		t.Fatalf("%+v", res)
	}
	if len(res.Errors) != 1 || res.Errors[0] != "@ref broken-login failed" {
		t.Fatalf("errors = %v", res.Errors)
	}
	if res.Request.Name != "after-broken" || res.Req() == nil {
		t.Fatalf("the failed result should still describe its request: %+v", res.Request)
	}
}

func TestRefErrors(t *testing.T) {
	dir, _ := refProject(t)
	ctx := context.Background()
	r := newRunner(t, dir, Options{Env: "dev", Session: session.NewMemory()})
	cases := map[string]string{
		"dangling": `@ref nope: no request named "nope"`,
		"cycle-a":  "@ref cycle-a is a cycle: cycle-a -> cycle-b -> cycle-a",
		"selfish":  "@ref selfish is a cycle: selfish -> selfish",
	}
	for name, want := range cases {
		res, err := r.Run(ctx, lookup(t, r, name))
		var ue *UsageError
		if !errors.As(err, &ue) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want usage error containing %q, got %v", name, want, err)
		}
		if res != nil && res.OK {
			t.Errorf("%s: result should not be ok: %+v", name, res)
		}
	}
	// Without a ref the hint still tells the user what to run.
	dir2 := writeProject(t, map[string]string{
		"api.http":             strings.ReplaceAll(refHTTP, "# @ref login\n", ""),
		"http-client.env.json": `{"dev": {"baseUrl": "http://127.0.0.1:9"}}`,
	})
	r2 := newRunner(t, dir2, Options{Env: "dev", Session: session.NewMemory()})
	_, err := r2.Run(ctx, lookup(t, r2, "whoami"))
	if err == nil || !strings.Contains(err.Error(), `captured by request "login"`) {
		t.Fatalf("want the capture hint, got %v", err)
	}
}

func TestDescribeReportsRefs(t *testing.T) {
	dir, _ := refProject(t)
	r := newRunner(t, dir, Options{Env: "dev", Session: session.NewMemory()})
	d := r.Describe(lookup(t, r, "whoami"))
	if len(d.Refs) != 1 || d.Refs[0] != "login" {
		t.Fatalf("refs = %v", d.Refs)
	}
	var token *VarInfo
	for i := range d.Variables {
		if d.Variables[i].Name == "token" {
			token = &d.Variables[i]
		}
	}
	if token == nil || !token.Missing || token.CapturedBy != "login" || !token.RefRuns {
		t.Fatalf("token = %+v", token)
	}
	d = r.Describe(lookup(t, r, "region"))
	if len(d.Refs) != 0 {
		t.Fatalf("region has no refs, got %v", d.Refs)
	}
}

// TestRefDiamondRunsSharedDependencyOnce: two dependencies that both lead
// to login share the "already ran" set of the invocation, so login goes
// out once even though it is reachable twice.
func TestRefDiamondRunsSharedDependencyOnce(t *testing.T) {
	dir, logins := refProject(t)
	r := newRunner(t, dir, Options{Env: "dev", Session: session.NewMemory()})
	res, err := r.Run(context.Background(), lookup(t, r, "diamond"))
	if err != nil || !res.OK {
		t.Fatalf("%+v err=%v", res, err)
	}
	if logins.Load() != 1 {
		t.Fatalf("login ran %d times, want 1", logins.Load())
	}
	// whoami (with login under it), then whoami-in-region (with only region
	// under it: whoami had already run).
	if n := names(res.Deps); len(n) != 2 || n[0] != "whoami" || n[1] != "whoami-in-region" {
		t.Fatalf("deps = %v", n)
	}
	if n := names(res.Deps[1].Deps); len(n) != 1 || n[0] != "region" {
		t.Fatalf("nested deps of whoami-in-region = %v, want only region", n)
	}
}
