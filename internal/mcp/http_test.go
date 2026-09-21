package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if b.token != "" {
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.base.RoundTrip(r)
}

func TestHTTPTransportBehindToken(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte("### ping\n# @name ping\nGET {{baseUrl}}/ping\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"http://api.example.test"}}`), 0o644))
	h, err := Handler(Config{Dir: dir, Env: "dev", Version: "test"}, "s3cret-token")
	must(t, err)
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx := context.Background()

	connect := func(token string) (*sdk.ClientSession, error) {
		client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
		tr := &sdk.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: &http.Client{Transport: bearer{token, http.DefaultTransport}}, MaxRetries: -1}
		return client.Connect(ctx, tr, nil)
	}
	if _, err := connect(""); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("no token should be refused with 401, got %v", err)
	}
	if _, err := connect("wrong"); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("wrong token should be refused with 401, got %v", err)
	}
	cs, err := connect("s3cret-token")
	must(t, err)
	defer func() { _ = cs.Close() }()
	tools, err := cs.ListTools(ctx, nil)
	must(t, err)
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	if len(tools.Tools) != 9 || !names["validate_project"] || !names["curl_request"] {
		t.Fatalf("tools over HTTP: %v", names)
	}
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "curl_request", Arguments: map[string]any{"name": "ping", "raw": true}})
	must(t, err)
	if text := res.Content[0].(*sdk.TextContent).Text; !strings.Contains(text, "http://api.example.test/ping") {
		t.Fatalf("curl over HTTP: %s", text)
	}
	// A plain request without a token sees nothing but the 401.
	resp, err := http.Get(srv.URL) //nolint:noctx // test
	must(t, err)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("GET without token: %d", resp.StatusCode)
	}
}

func TestCheckBind(t *testing.T) {
	ok := []string{"127.0.0.1:8765", "localhost:8765", "[::1]:8765", "127.0.0.2:0"}
	for _, addr := range ok {
		if err := CheckBind(addr, ""); err != nil {
			t.Errorf("%s without a token should be allowed: %v", addr, err)
		}
	}
	refused := []string{"0.0.0.0:8765", ":8765", "[::]:8765", "10.0.0.5:8765", "myhost:8765"}
	for _, addr := range refused {
		if err := CheckBind(addr, ""); err == nil || !strings.Contains(err.Error(), "--token") {
			t.Errorf("%s without a token should be refused, got %v", addr, err)
		}
		if err := CheckBind(addr, "tok"); err != nil {
			t.Errorf("%s with a token should be allowed: %v", addr, err)
		}
	}
	if err := CheckBind("nonsense", "tok"); err == nil {
		t.Error("a bare word is not host:port")
	}
}

func TestServeHTTPListensAndStops(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte("### ping\n# @name ping\nGET http://x/ping\n"), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	bound := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- ServeHTTP(ctx, Config{Dir: dir, Version: "test"}, "127.0.0.1:0", "", func(addr string) { bound <- addr })
	}()
	addr := <-bound
	resp, err := http.Get("http://" + addr + "/") //nolint:noctx // test
	must(t, err)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("no token on loopback should not demand one")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
	if err := ServeHTTP(context.Background(), Config{Dir: dir}, "0.0.0.0:0", "", nil); err == nil {
		t.Fatal("0.0.0.0 without a token must not start")
	}
}
