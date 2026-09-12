package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
)

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["user"] != "alice" || in["password"] != "s3cret" {
			http.Error(w, `{"error":"bad credentials"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-123","expires":3600}`))
	})
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-123" {
			http.Error(w, `{"error":"unauthorised"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":` + r.PathValue("id") + `,"email":"alice@example.com"}`))
	})
	mux.HandleFunc("GET /redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/users/1", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const apiHTTP = `
### Log in
# @name login
# @assert status == 200
# @capture token = body.$.access_token
POST {{baseUrl}}/auth/login
Content-Type: application/json

{"user": "{{user}}", "password": "{{password}}"}

### Get user
# @name get-user
# @assert status == 200
# @assert body.$.id == {{userId}}
# @assert header.content-type contains json
# @capture email = body.$.email
GET {{baseUrl}}/users/{{userId}}
Authorization: Bearer {{token}}

### Flow reference
# @name get-user-ref
GET {{baseUrl}}/users/2
Authorization: Bearer {{login.response.body.$.access_token}}

### Redirect
# @name redirect
# @no-redirect
# @assert status == 302
# @assert header.location == /users/1
GET {{baseUrl}}/redirect
`

func newRunner(t *testing.T, dir string, opts Options) *Runner {
	t.Helper()
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLoginCaptureAndReuseAcrossInvocations(t *testing.T) {
	srv := testServer(t)
	dir := writeProject(t, map[string]string{
		"api.http":                     apiHTTP,
		"http-client.env.json":         `{"dev": {"baseUrl": "` + srv.URL + `", "user": "alice", "userId": 7}}`,
		"http-client.private.env.json": `{"dev": {"password": "s3cret"}}`,
	})
	ctx := context.Background()

	// First invocation: get-user fails with a helpful hint because token is missing.
	r := newRunner(t, dir, Options{Env: "dev"})
	req, err := r.Project.Lookup("get-user")
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Run(ctx, req)
	var ue *UsageError
	if !errors.As(err, &ue) || !strings.Contains(err.Error(), `captured by request "login"`) {
		t.Fatalf("want missing-variable hint, got %v", err)
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("exit code %d", ExitCode(err))
	}

	// Log in: token captured and persisted.
	login, _ := r.Project.Lookup("login")
	res, err := r.Run(ctx, login)
	if err != nil || !res.OK || res.Captures["token"] != "tok-123" {
		t.Fatalf("login: %+v err=%v", res, err)
	}

	// Second invocation: a fresh runner picks the token up from the session.
	r2 := newRunner(t, dir, Options{Env: "dev"})
	res, err = r2.Run(ctx, req)
	if err != nil || !res.OK {
		t.Fatalf("get-user: %+v err=%v", res, err)
	}
	if res.Response.Status != 200 || res.Captures["email"] != "alice@example.com" || len(res.Asserts) != 3 {
		t.Fatalf("%+v", res)
	}
	if _, isJSON := res.Response.Body.(json.RawMessage); !isJSON {
		t.Fatalf("body should be raw JSON, got %T", res.Response.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, ".apic", ".gitignore")); err != nil {
		t.Fatal("session .gitignore not written")
	}

	// Describe reports sources.
	d := r2.Describe(req)
	src := map[string]string{}
	for _, v := range d.Variables {
		src[v.Name] = v.Source
	}
	if src["token"] != "session" || !strings.Contains(src["baseUrl"], "http-client.env.json [dev]") || !d.Ready {
		t.Fatalf("describe: %+v", d.Variables)
	}
	if !strings.HasPrefix(d.URL, srv.URL+"/users/7") {
		t.Fatalf("url %q", d.URL)
	}

	// --var beats everything and wrong id fails the assert (exit 1 territory, no error).
	r3 := newRunner(t, dir, Options{Env: "dev", Vars: map[string]string{"userId": "9"}})
	res, err = r3.Run(ctx, req)
	if err != nil || !res.OK || !strings.HasSuffix(res.Request.URL, "/users/9") {
		t.Fatalf("%+v %v", res, err)
	}
	r4 := newRunner(t, dir, Options{Env: "dev", Vars: map[string]string{"token": "wrong"}})
	res, err = r4.Run(ctx, req)
	if err != nil || res.OK || res.Asserts[0].Pass {
		t.Fatalf("expected failed assert: %+v %v", res, err)
	}
}

func TestFlowResponseReferenceAndNoRedirect(t *testing.T) {
	srv := testServer(t)
	dir := writeProject(t, map[string]string{
		"api.http": apiHTTP,
		".env":     "password=s3cret\n",
	})
	t.Setenv("APIC_VAR_baseUrl", srv.URL)
	r := newRunner(t, dir, Options{Vars: map[string]string{"user": "alice", "userId": "1"}, NoSession: true})
	results, err := r.RunAll(context.Background(), r.Project.Requests())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("ran %d", len(results))
	}
	for _, res := range results {
		if !res.OK {
			t.Errorf("%s failed: %+v", res.Request.Name, res)
		}
	}
	if results[2].Response.Status != 200 {
		t.Errorf("response reference did not resolve: %+v", results[2])
	}
	if _, err := os.Stat(filepath.Join(dir, ".apic")); !os.IsNotExist(err) {
		t.Error("NoSession must not write .apic")
	}
}

func TestFlowStopsOnFailure(t *testing.T) {
	srv := testServer(t)
	dir := writeProject(t, map[string]string{"api.http": apiHTTP,
		"http-client.env.json": `{"dev": {"baseUrl": "` + srv.URL + `", "user": "alice", "userId": 7, "password": "nope"}}`})
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true})
	results, err := r.RunAll(context.Background(), r.Project.Requests())
	if err != nil || len(results) != 1 || results[0].OK {
		t.Fatalf("want stop after failed login: %d results, err=%v", len(results), err)
	}
	if results[0].Errors == nil || !strings.Contains(results[0].Errors[0], "capture token") {
		t.Fatalf("want capture error, got %+v", results[0].Errors)
	}
}

func TestTransportError(t *testing.T) {
	dir := writeProject(t, map[string]string{"api.http": "GET http://127.0.0.1:1/nope\n"})
	r := newRunner(t, dir, Options{NoSession: true})
	_, err := r.Run(context.Background(), r.Project.Requests()[0])
	if ExitCode(err) != ExitTransport {
		t.Fatalf("want transport error, got %v", err)
	}
}

func TestUnknownEnv(t *testing.T) {
	dir := writeProject(t, map[string]string{"api.http": "GET http://x\n", "http-client.env.json": `{"dev": {}}`})
	p, _ := project.Load(dir)
	if _, err := New(p, Options{Env: "prod"}); err == nil || !strings.Contains(err.Error(), "have: dev") {
		t.Fatalf("got %v", err)
	}
}
