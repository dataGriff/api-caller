package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dataGriff/api-caller/internal/session"
)

// A JetBrains project's {{$auth.token("name")}} runs the Security.Auth
// grant once, through the session's token cache, and describe names the
// configuration without fetching.
func TestJetBrainsAuthToken(t *testing.T) {
	var grants atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		grants.Add(1)
		if err := r.ParseForm(); err != nil || r.PostForm.Get("client_secret") != "s3cret" || r.PostForm.Get("audience") != "api://x" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","id_token":"id-1","expires_in":3600}`))
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Header.Get("Authorization") + "|" + r.Header.Get("X-Id")))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	dir := writeProject(t, map[string]string{
		"api.http": `
### me
# @name me
GET {{baseUrl}}/me
Authorization: Bearer {{$auth.token("api")}}
X-Id: {{$auth.idToken("api")}}

### unknown
# @name unknown
GET {{baseUrl}}/me
Authorization: Bearer {{$auth.token("nope")}}
`,
		"http-client.env.json": `{"dev": {"baseUrl": "` + srv.URL + `", "Security": {"Auth": {"api": {
  "Type": "OAuth2", "Grant Type": "Client Credentials", "Token URL": "{{baseUrl}}/token",
  "Client ID": "cli", "Custom Request Parameters": {"audience": "api://x"}, "Acquire Automatically": true}}}}}`,
		"http-client.private.env.json": `{"dev": {"Security": {"Auth": {"api": {"Client Secret": "s3cret"}}}}}`,
	})
	sess := session.NewMemory()
	r := newRunner(t, dir, Options{Env: "dev", Session: sess})
	me, _ := r.Project.Lookup("me")
	d := r.Describe(me)
	if grants.Load() != 0 || !d.Ready {
		t.Fatalf("describe fetched a token or is not ready: grants=%d %+v", grants.Load(), d.Variables)
	}
	var src string
	for _, v := range d.Variables {
		if v.Name == `$auth.token("api")` {
			src = v.Source
		}
	}
	if !strings.Contains(src, `Security.Auth "api"`) || !strings.Contains(src, "grant=client_credentials") || strings.Contains(src, "s3cret") {
		t.Fatalf("describe source: %q", src)
	}
	res, err := r.Run(context.Background(), me)
	if err != nil || !res.OK {
		t.Fatalf("%+v %v", res, err)
	}
	if got := string(res.Raw().Body); got != "Bearer at-1|id-1" {
		t.Fatalf("server saw %q", got)
	}
	if grants.Load() != 1 {
		t.Fatalf("one grant serves both placeholders, got %d", grants.Load())
	}
	// A second runner (a later invocation) reuses the cached token.
	r2 := newRunner(t, dir, Options{Env: "dev", Session: sess})
	if res, err := r2.Run(context.Background(), me); err != nil || string(res.Raw().Body) != "Bearer at-1|id-1" || grants.Load() != 1 {
		t.Fatalf("cached: %v %v grants=%d", res, err, grants.Load())
	}
	// Tokens are secrets: the header shows masked.
	for _, h := range res.Request.DisplayHeaders(false) {
		if strings.Contains(h.Value, "at-1") || strings.Contains(h.Value, "id-1") {
			t.Fatalf("token shown: %s: %s", h.Name, h.Value)
		}
	}
	unknown, _ := r.Project.Lookup("unknown")
	if _, err := r.Run(context.Background(), unknown); err == nil || !strings.Contains(err.Error(), `no Security.Auth configuration of that name for environment "dev" (declared: api)`) {
		t.Fatalf("unknown: %v", err)
	}
	if d := r.Describe(unknown); d.Ready {
		t.Fatal("an unknown configuration makes the request unready")
	}
	cfgs := r.AuthConfigs()
	if len(cfgs) != 1 || cfgs[0].Name != "api" || strings.Contains(cfgs[0].Spec, "s3cret") || !strings.Contains(cfgs[0].Spec, "clientSecret=***") {
		t.Fatalf("configs: %+v", cfgs)
	}
}
