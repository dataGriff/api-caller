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
	defer cs.Close()

	tools, err := cs.ListTools(ctx, nil)
	must(t, err)
	if len(tools.Tools) != 6 {
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
	if n := len(list["requests"].([]any)); n != 2 {
		t.Fatalf("list: %v", list)
	}
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
	if flow["ok"] != true || len(flow["results"].([]any)) != 2 {
		t.Fatalf("flow: %v", flow)
	}
	envs := call("list_environments", map[string]any{})
	if envs["current"] != "dev" {
		t.Fatalf("envs: %v", envs)
	}
	call("clear_session", map[string]any{})
	if d := call("describe_request", map[string]any{"name": "me"}); d["ready"] != false {
		t.Fatal("session should be cleared")
	}

	rr, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: "file://" + filepath.ToSlash(filepath.Join(dir, "api.http"))})
	must(t, err)
	if !strings.Contains(rr.Contents[0].Text, "@name login") {
		t.Fatal("resource content")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
