package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/httpfile"
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
	if d.URL != "{{baseUrl}}/users/{{userId}}" {
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

func TestNewRejectsParseDiagnostics(t *testing.T) {
	dir := t.TempDir()
	p := &project.Project{
		Root:        dir,
		Diagnostics: []httpfile.Diagnostic{{Path: "bad.http", Line: 3, Severity: "error", Message: "bad directive"}},
	}
	if _, err := New(p, Options{}); err == nil || !strings.Contains(err.Error(), "bad.http:3") {
		t.Fatalf("want parse diagnostic error, got %v", err)
	}
}

func TestRunPreflightsAssertTemplatesAndSkipsTransport(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	dir := writeProject(t, map[string]string{
		"api.http": `
# @name t
# @assert status == {{missing}}
GET ` + srv.URL + `
`,
	})
	r := newRunner(t, dir, Options{NoSession: true})
	_, err := r.Run(context.Background(), r.Project.Requests()[0])
	if ExitCode(err) != ExitUsage {
		t.Fatalf("want usage error, got %v", err)
	}
	if hits != 0 {
		t.Fatalf("request should not have been sent, hits=%d", hits)
	}
}

func TestRunAllKeepGoingReturnsFirstError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	dir := writeProject(t, map[string]string{"api.http": `
GET {{missing}}
###
GET ` + srv.URL + `
`})
	r := newRunner(t, dir, Options{NoSession: true, KeepGoing: true})
	results, err := r.RunAll(context.Background(), r.Project.Requests())
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if ExitCode(err) != ExitUsage {
		t.Fatalf("want usage error, got %v", err)
	}
}

func TestRunRejectsBodyFileOutsideProject(t *testing.T) {
	base := t.TempDir()
	secret := filepath.Join(base, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(base, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "api.http"), []byte(`
POST https://example.com

< ../secret.txt
`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newRunner(t, proj, Options{NoSession: true})
	_, err := r.Run(context.Background(), r.Project.Requests()[0])
	if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "outside project root") {
		t.Fatalf("want outside project error, got %v", err)
	}
}

func TestJSONOrStringAcceptsScalarJSON(t *testing.T) {
	for _, in := range []string{"true", "7", "null", `"x"`} {
		got := jsonOrString([]byte(in))
		if reflect.TypeOf(got) != reflect.TypeOf(json.RawMessage{}) {
			t.Fatalf("want RawMessage for %q, got %T", in, got)
		}
	}
}

func TestNestedFileVarMissingPropagates(t *testing.T) {
	dir := writeProject(t, map[string]string{"api.http": `
@base = {{missing}}
GET {{base}}/x
`})
	r := newRunner(t, dir, Options{NoSession: true})
	_, err := r.Run(context.Background(), r.Project.Requests()[0])
	if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "{{missing}}") {
		t.Fatalf("want missing variable error, got %v", err)
	}
}

func TestDescribeMarksDotenvBuiltinsSecret(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"api.http": `
# @name t
GET https://example.com?q={{$dotenv X}}
`,
		".env": "X=1",
	})
	r := newRunner(t, dir, Options{NoSession: true})
	d := r.Describe(r.Project.Requests()[0])
	if !d.Ready {
		t.Fatalf("expected request to be ready: %+v", d)
	}
	found := false
	for _, v := range d.Variables {
		if v.Name == "$dotenv X" {
			found = true
			if !v.Secret {
				t.Fatalf("dotenv variable should be secret: %+v", v)
			}
		}
	}
	if !found {
		t.Fatalf("dotenv variable not reported: %+v", d.Variables)
	}
}

func TestSessionSaveFailureMarksResultFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"x"}`))
	}))
	defer srv.Close()
	dir := writeProject(t, map[string]string{
		"api.http": `
# @name t
# @capture token = body.$.token
GET ` + srv.URL + `
`,
	})
	r := newRunner(t, dir, Options{})
	// Make session persistence fail deterministically: .apic must be a directory.
	if err := os.WriteFile(filepath.Join(dir, ".apic"), []byte("not-a-dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(context.Background(), r.Project.Requests()[0])
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatalf("result should fail on session save error: %+v", res)
	}
	if len(res.Errors) == 0 || !strings.Contains(res.Errors[0], "session:") {
		t.Fatalf("missing session save error: %+v", res.Errors)
	}
}

func TestResultJSONMasksSensitiveHeadersAndRedacts(t *testing.T) {
	res := Result{
		OK: true,
		Request: Resolved{
			Method: "GET",
			URL:    "https://example.com/users?token=secret&id=1",
			Headers: []httpfile.Header{
				{Name: "Authorization", Value: "Bearer secret-token"},
				{Name: "X-Tenant", Value: "from-private-file"},
				{Name: "Accept", Value: "application/json"},
			},
			SecretHeaders: map[string]bool{"X-Tenant": true},
			Body:          "plain-body",
		},
		Captures: map[string]string{"token": "captured"},
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, bad := range []string{"secret-token", "from-private-file"} {
		if strings.Contains(s, bad) {
			t.Fatalf("json should mask %q: %s", bad, s)
		}
	}
	var decoded struct {
		Request struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
		} `json:"request"`
		Captures map[string]string `json:"captures"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Request.URL != "https://example.com/users?token=secret&id=1" || decoded.Request.Headers["Accept"] != "application/json" ||
		decoded.Request.Body != "plain-body" || decoded.Captures["token"] != "captured" {
		t.Fatalf("json should keep url, plain headers, body and captures: %s", s)
	}

	res.Redact = true
	data, _ = json.Marshal(res)
	s = string(data)
	for _, bad := range []string{"secret", "plain-body", "captured", "application/json", "id=1"} {
		if strings.Contains(s, bad) {
			t.Fatalf("--redact should mask %q: %s", bad, s)
		}
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Request.URL != "https://example.com/users?token=***&id=***" || decoded.Captures["token"] != "***" {
		t.Fatalf("redacted json: %s", s)
	}
}

func TestResolveMarksSecretHeaders(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"api.http":                     "### a\n# @name a\nGET http://x/\nX-Tenant: {{tenant}}\nX-Plain: {{plain}}\nAuthorization: Bearer {{token}}\n",
		"http-client.env.json":         `{"dev": {"plain": "p"}}`,
		"http-client.private.env.json": `{"dev": {"tenant": "t", "token": "s"}}`,
	})
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true})
	res, err := r.Resolve(r.Project.Requests()[0])
	if err != nil {
		t.Fatal(err)
	}
	if !res.SecretHeaders["X-Tenant"] || res.SecretHeaders["X-Plain"] {
		t.Fatalf("secret headers: %v", res.SecretHeaders)
	}
	shown := map[string]string{}
	for _, h := range res.DisplayHeaders(false) {
		shown[h.Name] = h.Value
	}
	if shown["X-Tenant"] != Masked || shown["Authorization"] != Masked || shown["X-Plain"] != "p" {
		t.Fatalf("display headers: %v", shown)
	}
}
