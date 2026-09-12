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
