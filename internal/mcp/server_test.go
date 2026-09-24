package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServerEndToEnd(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"t-1"}`))
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t-1" {
			w.WriteHeader(401)
			return
		}

		_, _ = w.Write([]byte(`{"name":"alice"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte(`
### login
# @name login
# @capture token = body.$.token
POST {{baseUrl}}/login

### me
# @name me
# @assert status == 200
# @assert body.$.name == alice
GET {{baseUrl}}/me
Authorization: Bearer {{token}}

### me, logging in by itself
# @name me-ref
# @ref login
# @assert status == 200
GET {{baseUrl}}/me
Authorization: Bearer {{token}}
`), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`"}}`), 0o644))

	server, err := New(Config{Dir: dir, Env: "dev", Version: "test"})
	must(t, err)
	ct, st := sdk.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = server.Run(ctx, st) }()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	must(t, err)
	defer func() { _ = cs.Close() }()

	tools, err := cs.ListTools(ctx, nil)
	must(t, err)
	if len(tools.Tools) != 9 {
		t.Fatalf("tools: %d", len(tools.Tools))
	}

	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		must(t, err)
		text := res.Content[0].(*sdk.TextContent).Text
		if res.IsError {
			return map[string]any{"_error": text}
		}
		var out map[string]any
		must(t, json.Unmarshal([]byte(text), &out))
		return out
	}

	list := call("list_requests", map[string]any{})
	if n := len(list["requests"].([]any)); n != 3 {
		t.Fatalf("list: %v", list)
	}
	// A request with # @ref logs in by itself and reports what ran first.
	meRef := call("run_request", map[string]any{"name": "me-ref"})
	ranFirst, _ := meRef["ran_first"].([]any)
	if meRef["ok"] != true || len(ranFirst) != 1 || ranFirst[0].(map[string]any)["ok"] != true {
		t.Fatalf("me-ref: %v", meRef)
	}
	call("clear_session", map[string]any{})
	desc := call("describe_request", map[string]any{"name": "me"})
	if desc["ready"] != false {
		t.Fatalf("me should not be ready before login: %v", desc)
	}
	if e := call("run_request", map[string]any{"name": "me"}); !strings.Contains(e["_error"].(string), "login") {
		t.Fatalf("want missing-variable hint, got %v", e)
	}
	login := call("run_request", map[string]any{"name": "login"})
	if login["ok"] != true || login["captures"].(map[string]any)["token"] != "t-1" {
		t.Fatalf("login: %v", login)
	}
	me := call("run_request", map[string]any{"name": "me"})
	if me["ok"] != true {
		t.Fatalf("me: %v", me)
	}
	if e := call("run_request", map[string]any{"name": "me", "vars": map[string]string{"token": "bad"}}); e["ok"] != false {
		t.Fatalf("bad token should fail asserts: %v", e)
	}
	flow := call("run_file", map[string]any{"file": "api.http"})
	if flow["ok"] != true || len(flow["results"].([]any)) != 3 {
		t.Fatalf("flow: %v", flow)
	}
	// The listing carries the # @ref targets.
	for _, e := range list["requests"].([]any) {
		if r := e.(map[string]any); r["id"] == "me-ref" {
			if refs, _ := r["refs"].([]any); len(refs) != 1 || refs[0] != "login" {
				t.Fatalf("me-ref refs: %v", r)
			}
		}
	}
	valid := call("validate_project", map[string]any{})
	if valid["ok"] != true || valid["files"] != float64(1) || valid["requests"] != float64(3) || len(valid["diagnostics"].([]any)) != 0 {
		t.Fatalf("validate_project: %v", valid)
	}
	must(t, os.WriteFile(filepath.Join(dir, "bad.http"), []byte("### bad\n# @name bad\n# @assert stauts == 200\nGET {{baseUrl}}/x\n"), 0o644))
	valid = call("validate_project", map[string]any{})
	if valid["ok"] != false || len(valid["diagnostics"].([]any)) != 1 {
		t.Fatalf("validate_project after a bad file: %v", valid)
	}
	if d := valid["diagnostics"].([]any)[0].(map[string]any); d["path"] != "bad.http" || d["line"] != float64(3) || d["column"] != float64(11) || d["code"] != "unknown-selector" {
		t.Fatalf("diagnostic: %v", d)
	}
	must(t, os.Remove(filepath.Join(dir, "bad.http")))
	// Masked by default, live only when asked, so the command is safe to
	// paste into a log or a ticket as the tool hands it over.
	curl := call("curl_request", map[string]any{"name": "me"})
	if cmd, _ := curl["command"].(string); !strings.HasPrefix(cmd, "curl -sS") || strings.Contains(cmd, "t-1") || !strings.Contains(cmd, "***") || !strings.Contains(cmd, srv.URL+"/me") {
		t.Fatalf("curl_request: %v", curl)
	}
	curl = call("curl_request", map[string]any{"name": "me", "raw": true})
	if cmd, _ := curl["command"].(string); !strings.Contains(cmd, "Authorization: Bearer t-1") {
		t.Fatalf("curl_request raw: %v", curl)
	}
	py := call("curl_request", map[string]any{"name": "me", "lang": "python"})
	if code, _ := py["command"].(string); py["lang"] != "python" || !strings.Contains(code, "requests.request(") || strings.Contains(code, "t-1") {
		t.Fatalf("curl_request python: %v", py)
	}
	if e := call("curl_request", map[string]any{"name": "me", "lang": "cobol"}); !strings.Contains(e["_error"].(string), "unknown language") {
		t.Fatalf("curl_request cobol: %v", e)
	}
	// A missing variable is an error naming it, never a command with a
	// placeholder left in.
	call("clear_session", map[string]any{})
	if e := call("curl_request", map[string]any{"name": "me"}); !strings.Contains(e["_error"].(string), "login") {
		t.Fatalf("curl_request with a missing variable: %v", e)
	}
	call("run_request", map[string]any{"name": "login"})
	if e := call("curl_request", map[string]any{"name": "nope"}); !strings.Contains(e["_error"].(string), "nope") {
		t.Fatalf("curl_request unknown: %v", e)
	}
	envs := call("list_environments", map[string]any{})
	if envs["current"] != "dev" {
		t.Fatalf("envs: %v", envs)
	}
	call("clear_session", map[string]any{})
	if d := call("describe_request", map[string]any{"name": "me"}); d["ready"] != false {
		t.Fatal("session should be cleared")
	}

	must(t, os.MkdirAll(filepath.Join(dir, "features"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "features", "me.feature"), []byte(`
Feature: Me
  Scenario: Login then me
    Given I run "login"
    When I run "me"
    Then the response status is 200
    And the response body "$.name" is "alice"
  Scenario: Fails
    When I run "login"
    Then the response status is 500
`), 0o644))
	feat := call("run_features", map[string]any{})
	if feat["ok"] != false || feat["passed"] != float64(1) || feat["failed"] != float64(1) || feat["exit_code"] != float64(1) {
		t.Fatalf("run_features: %v", feat)
	}
	// Isolated by default: the feature's login capture never reaches the shared session.
	if d := call("describe_request", map[string]any{"name": "me"}); d["ready"] != false {
		t.Fatal("run_features must not touch the shared session by default")
	}
	shared := call("run_features", map[string]any{"use_session": true, "tags": "~@none"})
	if shared["scenarios"] != float64(2) {
		t.Fatalf("run_features with use_session: %v", shared)
	}
	if d := call("describe_request", map[string]any{"name": "me"}); d["ready"] != true {
		t.Fatalf("use_session must share captures with the other tools: %v", d)
	}
	call("clear_session", map[string]any{})
	// A transport failure still returns the summary of what ran, with the error and exit code.
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"`+srv.URL+`"},"down":{"baseUrl":"http://127.0.0.1:1"}}`), 0o644))
	down := call("run_features", map[string]any{"env": "down"})
	if down["ok"] != false || down["exit_code"] != float64(3) || down["scenarios"] != float64(2) || !strings.Contains(down["error"].(string), "request failed") {
		t.Fatalf("run_features transport: %v", down)
	}
	if f := feat["failures"].([]any)[0].(map[string]any); f["scenario"] != "Fails" || !strings.Contains(f["error"].(string), "status == 500") {
		t.Fatalf("failure detail: %v", f)
	}

	rr, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: "file://" + filepath.ToSlash(filepath.Join(dir, "api.http"))})
	must(t, err)
	if !strings.Contains(rr.Contents[0].Text, "@name login") {
		t.Fatal("resource content")
	}
}

func TestReadFileRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	must(t, os.WriteFile(target, []byte("x"), 0o644))
	link := filepath.Join(root, "leak.txt")
	must(t, os.Symlink(target, link))
	s := &service{root: root}
	_, err := s.readFile(context.Background(), &sdk.ReadResourceRequest{
		Params: &sdk.ReadResourceParams{URI: "file://" + filepath.ToSlash(link)},
	})
	if err == nil || !strings.Contains(err.Error(), "outside project") {
		t.Fatalf("expected outside-project rejection, got %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestReadFileServesOnlyProjectFiles pins that the resource reader is not a
// general "any file under the root" reader. The private env file, .env and
// the session cache all live under the project root and hold credentials.
func TestReadFileServesOnlyProjectFiles(t *testing.T) {
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "api.http"), []byte("# @name get\nGET https://example.com/x\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(root, ".apic"), 0o700))
	secrets := map[string]string{
		"http-client.private.env.json": `{"dev":{"token":"super-secret"}}`,
		".env":                         "API_KEY=super-secret\n",
		".apic/session.json":           `{"dev":{"token":"super-secret"}}`,
		"notes.txt":                    "super-secret",
	}
	for name, content := range secrets {
		must(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o600))
	}

	s := &service{root: root}
	for name := range secrets {
		uri := "file://" + filepath.ToSlash(filepath.Join(root, filepath.FromSlash(name)))
		res, err := s.readFile(context.Background(), &sdk.ReadResourceRequest{Params: &sdk.ReadResourceParams{URI: uri}})
		if err == nil {
			t.Fatalf("%s should not be readable as a resource, got %+v", name, res)
		}
		if strings.Contains(err.Error(), "super-secret") {
			t.Fatalf("%s: error should not echo the file contents: %v", name, err)
		}
	}

	// The project's own .http file is still served.
	uri := "file://" + filepath.ToSlash(filepath.Join(root, "api.http"))
	res, err := s.readFile(context.Background(), &sdk.ReadResourceRequest{Params: &sdk.ReadResourceParams{URI: uri}})
	if err != nil {
		t.Fatalf("api.http should be readable: %v", err)
	}
	if len(res.Contents) != 1 || !strings.Contains(res.Contents[0].Text, "GET https://example.com/x") {
		t.Fatalf("unexpected contents: %+v", res.Contents)
	}
}

// TestReadFileServesFileAddedAfterStart pins that the allowlist is rebuilt per
// read rather than snapshotted at New, matching the tools, which re-load the
// project on every call.
func TestReadFileServesFileAddedAfterStart(t *testing.T) {
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "api.http"), []byte("# @name get\nGET https://example.com/x\n"), 0o644))
	if _, err := New(Config{Dir: root}); err != nil {
		t.Fatal(err)
	}
	must(t, os.WriteFile(filepath.Join(root, "late.http"), []byte("# @name late\nGET https://example.com/late\n"), 0o644))

	s := &service{root: root}
	uri := "file://" + filepath.ToSlash(filepath.Join(root, "late.http"))
	res, err := s.readFile(context.Background(), &sdk.ReadResourceRequest{Params: &sdk.ReadResourceParams{URI: uri}})
	if err != nil {
		t.Fatalf("a file added after start should be readable: %v", err)
	}
	if len(res.Contents) != 1 || !strings.Contains(res.Contents[0].Text, "example.com/late") {
		t.Fatalf("unexpected contents: %+v", res.Contents)
	}
}
