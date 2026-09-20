package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/session"
)

const cookieHTTP = `
### Log in with a form; the server answers with a session cookie
# @name form-login
# @assert status == 204
# @assert cookie.sid exists
# @capture sid = cookie.sid
POST {{baseUrl}}/login

### Only a browser-style session gets in
# @name me
# @assert status == 200
GET {{baseUrl}}/me

### The same request without the jar
# @name me-anon
# @no-cookies
# @assert status == 401
GET {{baseUrl}}/me
`

func cookieServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s-1", Path: "/", HttpOnly: true})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sid"); err != nil || c.Value != "s-1" {
			http.Error(w, "no session", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user":"alice"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestCookieJarIsOptInPersistedPerEnvironmentAndSkippable(t *testing.T) {
	srv := cookieServer(t)
	dir := writeProject(t, map[string]string{
		"api.http":             cookieHTTP,
		"http-client.env.json": `{"dev": {"baseUrl": "` + srv.URL + `"}, "staging": {"baseUrl": "` + srv.URL + `"}}`,
	})
	run := func(r *Runner, name string) *Result {
		t.Helper()
		reqs, _ := r.Project.Resolve(name)
		res, err := r.Run(context.Background(), reqs[0])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res
	}

	// Off by default: the cookie is visible to selectors but never sent.
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true})
	if r.Jar != nil {
		t.Fatal("jar on without --cookies")
	}
	if res := run(r, "form-login"); !res.OK || res.Captures["sid"] != "s-1" {
		t.Fatalf("login without jar: %+v", res)
	}
	if res := run(r, "me"); res.OK || res.Response.Status != 401 {
		t.Fatalf("me without jar should be 401: %+v", res)
	}

	// On: the cookie is sent with later requests, and survives the process.
	r = newRunner(t, dir, Options{Env: "dev", Cookies: true})
	if res := run(r, "form-login"); !res.OK {
		t.Fatalf("login: %+v", res)
	}
	if res := run(r, "me"); !res.OK {
		t.Fatalf("me with jar: %+v", res)
	}
	if res := run(r, "me-anon"); !res.OK {
		t.Fatalf("@no-cookies should send none: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, ".apic", session.CookieFile)); err != nil {
		t.Fatalf("jar not persisted: %v", err)
	}
	r2 := newRunner(t, dir, Options{Env: "dev", Cookies: true})
	if res := run(r2, "me"); !res.OK {
		t.Fatalf("me in a new process: %+v", res)
	}
	if got := r2.Jar.Cookies("dev"); len(got) != 1 || got[0].Name != "sid" || !got[0].HTTPOnly {
		t.Errorf("stored = %+v", got)
	}

	// Per environment: staging has its own, empty, jar.
	r3 := newRunner(t, dir, Options{Env: "staging", Cookies: true})
	if res := run(r3, "me"); res.OK {
		t.Fatalf("staging should not see dev's cookie: %+v", res)
	}

	// apic.yaml switches it on too; --no-session keeps the jar in memory
	// for the run without touching the file.
	if err := os.WriteFile(filepath.Join(dir, "apic.yaml"), []byte("cookies: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r4 := newRunner(t, dir, Options{Env: "staging", NoSession: true})
	if r4.Jar == nil {
		t.Fatal("apic.yaml cookies: true should open a jar")
	}
	run(r4, "form-login")
	if res := run(r4, "me"); !res.OK {
		t.Fatalf("in-memory jar within a run: %+v", res)
	}
	r5 := newRunner(t, dir, Options{Env: "staging", Cookies: true})
	if res := run(r5, "me"); res.OK {
		t.Fatalf("--no-session must not persist cookies: %+v", res)
	}

	// The jar is redacted in output like any other cookie header.
	r6 := newRunner(t, dir, Options{Env: "dev", Cookies: true, Redact: true})
	res := run(r6, "form-login")
	if h := res.DisplayResponse().Headers["set-cookie"]; h != Masked {
		t.Errorf("set-cookie shown as %q", h)
	}
	data, _ := res.MarshalJSON()
	if strings.Contains(string(data), "s-1") {
		t.Errorf("cookie value leaked into redacted output: %s", data)
	}
}
