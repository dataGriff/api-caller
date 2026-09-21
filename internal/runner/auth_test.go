package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
)

func TestAuthDirectiveDefaultAndNone(t *testing.T) {
	var tokenCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "o-tok", "expires_in": 3600})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"authz": r.Header.Get("Authorization"), "key": r.Header.Get("X-Api-Key")})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := writeProject(t, map[string]string{
		"apic.yaml":                    "auth:\n  default: bearer {{token}}\n  allowExec: true\n",
		"http-client.env.json":         `{"dev": {"baseUrl": "` + srv.URL + `", "user": "alice"}}`,
		"http-client.private.env.json": `{"dev": {"password": "pw", "token": "default-tok", "clientSecret": "sec"}}`,
		"api.http": `
### default
# @name default
# @assert body.$.authz == "Bearer default-tok"
GET {{baseUrl}}/a

### none
# @name none
# @assert body.$.authz == ""
# @auth none
GET {{baseUrl}}/b

### basic
# @name basic
# @auth basic {{user}} {{password}}
GET {{baseUrl}}/c

### oauth2
# @name oauth2
# @auth oauth2 tokenUrl={{baseUrl}}/token clientId=cid clientSecret={{clientSecret}}
# @assert body.$.authz == "Bearer o-tok"
GET {{baseUrl}}/d

### exec
# @name exec
# @auth exec go env GOOS header=X-Api-Key prefix=
GET {{baseUrl}}/e

### missing
# @name missing
# @auth bearer {{nope}}
GET {{baseUrl}}/f
`,
	})
	ctx := context.Background()
	r := newRunner(t, dir, Options{Env: "dev"})
	run := func(name string) *Result {
		t.Helper()
		req, err := r.Project.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		res, err := r.Run(ctx, req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !res.OK {
			t.Fatalf("%s: %+v", name, res)
		}
		return res
	}
	if res := run("default"); res.Request.Auth != "bearer" {
		t.Fatalf("auth type %q", res.Request.Auth)
	}
	run("none")
	res := run("basic")
	var body map[string]string
	_ = json.Unmarshal(res.Raw().Body, &body)
	if body["authz"] != "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:pw")) {
		t.Fatalf("basic: %v", body)
	}
	run("oauth2")
	run("oauth2")
	if tokenCalls.Load() != 1 {
		t.Fatalf("token fetched %d times; should be cached in session", tokenCalls.Load())
	}
	// A new runner reuses the cached token from the session file.
	r2 := newRunner(t, dir, Options{Env: "dev"})
	req, _ := r2.Project.Lookup("oauth2")
	if res, err := r2.Run(ctx, req); err != nil || !res.OK || tokenCalls.Load() != 1 {
		t.Fatalf("session cache: %v calls=%d", err, tokenCalls.Load())
	}
	res = run("exec")
	_ = json.Unmarshal(res.Raw().Body, &body)
	if body["key"] == "" || strings.Contains(body["key"], " ") {
		t.Fatalf("exec: %v", body)
	}

	// Missing variable inside the auth spec is reported like any other.
	req, _ = r.Project.Lookup("missing")
	if _, err := r.Run(ctx, req); err == nil || !strings.Contains(err.Error(), "{{nope}}") {
		t.Fatalf("want missing variable error, got %v", err)
	}

	// Describe reports the auth spec and its variables.
	req, _ = r.Project.Lookup("basic")
	d := r.Describe(req)
	if d.Auth != "basic {{user}} {{password}}" || d.AuthSource != "request" {
		t.Fatalf("describe auth: %+v", d)
	}
	names := map[string]bool{}
	for _, v := range d.Variables {
		names[v.Name] = true
	}
	if !names["user"] || !names["password"] {
		t.Fatalf("describe variables: %+v", d.Variables)
	}
	req, _ = r.Project.Lookup("default")
	if d := r.Describe(req); d.AuthSource != project.ConfigFile {
		t.Fatalf("default auth source: %+v", d)
	}

	// Cached tokens are not listed as ordinary variables.
	for _, v := range r.EnvVars() {
		if strings.HasPrefix(v.Name, "$") {
			t.Fatalf("token cache leaked into env vars: %s", v.Name)
		}
	}
}

func TestAuthValidate(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"apic.yaml": "auth:\n  default: magic\n",
		"api.http":  "### a\n# @auth exec whoami\nGET http://x\n\n### b\n# @auth aws sigv4\nGET http://y\n",
	})
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range p.Validate() {
		msgs = append(msgs, d.Severity+": "+d.Message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{"error: auth.default", "warning: @auth exec will be refused", "error: @auth aws takes only"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestEmptyAuthDirectiveReturnsUsageErrorInsteadOfUsingDefault(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"apic.yaml": "auth:\n  default: bearer {{token}}\n",
		"api.http":  "### t\n# @auth\nGET http://example.com\n",
	})
	r := newRunner(t, dir, Options{Vars: map[string]string{"token": "tok"}, NoSession: true})
	reqs, err := r.Project.Resolve("api.http#1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), reqs[0]); err == nil || !strings.Contains(err.Error(), "@auth needs a type") {
		t.Fatalf("want empty @auth usage error, got %v", err)
	}
}

func TestInsecureAlsoAppliesToOAuthTokenClient(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 60})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"authz": r.Header.Get("Authorization")})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	dir := writeProject(t, map[string]string{
		"http-client.env.json":         `{"default": {"baseUrl": "` + srv.URL + `"}}`,
		"http-client.private.env.json": `{"default": {"clientSecret": "sec"}}`,
		"api.http": `
### oauth2
# @name oauth2
# @auth oauth2 tokenUrl={{baseUrl}}/token clientId=cid clientSecret={{clientSecret}}
GET {{baseUrl}}/
`,
	})
	r := newRunner(t, dir, Options{Env: "default", Insecure: true, NoSession: true})
	req, err := r.Project.Lookup("oauth2")
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("unexpected failed result: %+v", res)
	}
}

func TestDigestAuthAnswersChallengeThroughRun(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Digest ") {
			w.Header().Set("WWW-Authenticate", `Digest realm="apic", nonce="n1", algorithm=SHA-256, qop="auth"`)
			http.Error(w, `{"error":"challenge"}`, http.StatusUnauthorized)
			return
		}
		if !strings.Contains(authz, `username="alice"`) || !strings.Contains(authz, `uri="/secret?x=1"`) || !strings.Contains(authz, "nc=0000000") {
			http.Error(w, `{"error":"bad"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "nc": authz[strings.Index(authz, "nc=")+3 : strings.Index(authz, "nc=")+11]})
	}))
	defer srv.Close()
	dir := writeProject(t, map[string]string{
		"http-client.env.json":         `{"dev": {"baseUrl": "` + srv.URL + `", "user": "alice"}}`,
		"http-client.private.env.json": `{"dev": {"password": "s3cret"}}`,
		"api.http": `
### secret
# @name secret
# @auth digest {{user}} {{password}}
# @assert status == 200
# @assert body.$.ok == true
GET {{baseUrl}}/secret?x=1

### again
# @name again
# @auth digest {{user}} {{password}}
# @assert body.$.nc == "00000002"
GET {{baseUrl}}/secret?x=1
`,
	})
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true})
	req, _ := r.Project.Lookup("secret")
	res, err := r.Run(context.Background(), req)
	if err != nil || !res.OK {
		t.Fatalf("err=%v ok=%v problem=%s", err, res != nil && res.OK, res.Problem())
	}
	if res.Request.Auth != "digest" || res.AuthNote() != "digest: 401 challenge answered (2 requests)" {
		t.Fatalf("auth=%q note=%q", res.Request.Auth, res.AuthNote())
	}
	data, _ := json.Marshal(res)
	if !strings.Contains(string(data), `"auth":"digest"`) || strings.Contains(string(data), "s3cret") {
		t.Fatalf("json: %s", data)
	}
	// The second request in the same invocation is answered first time
	// with the next nonce count.
	req, _ = r.Project.Lookup("again")
	res, err = r.Run(context.Background(), req)
	if err != nil || !res.OK || res.AuthNote() != "digest: answered from an earlier challenge" {
		t.Fatalf("again: err=%v ok=%v note=%q problem=%s", err, res != nil && res.OK, res.AuthNote(), res.Problem())
	}
	if hits.Load() != 3 {
		t.Fatalf("server hits = %d, want 3 (challenge, answer, answer)", hits.Load())
	}
}

func TestAPIKeyAuthThroughRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"header": r.Header.Get("X-Api-Key"), "token": r.Header.Get("X-Auth-Token"), "query": r.URL.Query().Get("api_key")})
	}))
	defer srv.Close()
	dir := writeProject(t, map[string]string{
		"apic.yaml":                    "auth:\n  default: apikey {{apiKey}}\n",
		"http-client.env.json":         `{"dev": {"baseUrl": "` + srv.URL + `"}}`,
		"http-client.private.env.json": `{"dev": {"apiKey": "k-123"}}`,
		"api.http": `
### header
# @name header
# @assert body.$.header == "k-123"
GET {{baseUrl}}/a

### custom
# @name custom
# @auth apikey {{apiKey}} header=X-Auth-Token prefix="Token "
# @assert body.$.token == "Token k-123"
GET {{baseUrl}}/b

### query
# @name query
# @auth apikey {{apiKey}} query=api_key
# @assert body.$.query == "k-123"
GET {{baseUrl}}/c?keep=1
`,
	})
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true})
	for _, name := range []string{"header", "custom", "query"} {
		req, _ := r.Project.Lookup(name)
		res, err := r.Run(context.Background(), req)
		if err != nil || !res.OK {
			t.Fatalf("%s: err=%v problem=%s", name, err, res.Problem())
		}
		if res.Request.Auth != "apikey" {
			t.Fatalf("%s: auth = %q", name, res.Request.Auth)
		}
		// The server echoes the key in its body; the request side never
		// shows it, in any form.
		if data, _ := json.Marshal(res.Request); strings.Contains(string(data), "k-123") {
			t.Fatalf("%s: key in request output: %s", name, data)
		}
	}
}

func TestAuthorizationCodeNeedsATerminal(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"http-client.env.json": `{"dev": {"baseUrl": "http://api.example.test"}}`,
		"api.http": `
### me
# @name me
# @auth oauth2 grant=authorization_code authUrl=http://idp.example.test/authorize tokenUrl=http://idp.example.test/token clientId=cid
GET {{baseUrl}}/me
`,
	})
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true})
	req, _ := r.Project.Lookup("me")
	_, err := r.Run(context.Background(), req)
	if err == nil || ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "run the request once interactively") {
		t.Fatalf("err = %v (exit %d)", err, ExitCode(err))
	}
}
