package runner

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tlsServer answers with the protocol it served, over TLS, offering HTTP/2
// only when h2 is set.
func tlsServer(t *testing.T, h2 bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Proto))
	}))
	srv.EnableHTTP2 = h2
	if h2 {
		// Offer both, as a real server does; httptest offers h2 alone.
		srv.TLS = &tls.Config{NextProtos: []string{"h2", "http/1.1"}} //nolint:gosec // a test server
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// The HTTP version on the request line pins the protocol: HTTP/1.1 turns
// HTTP/2 off, HTTP/2 requires it, and none negotiates as before.
func TestRequestLineVersion(t *testing.T) {
	h2, h1 := tlsServer(t, true), tlsServer(t, false)
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(plain.Close)
	dir := writeProject(t, map[string]string{"api.http": `
### any
# @name any
GET {{h2}}/

### one
# @name one
GET {{h2}}/ HTTP/1.1

### two
# @name two
GET {{h2}}/ HTTP/2

### two-refused
# @name two-refused
GET {{h1}}/ HTTP/2

### two-plain
# @name two-plain
GET {{plain}}/ HTTP/2
`, "http-client.env.json": `{"dev": {"h2": "` + h2.URL + `", "h1": "` + h1.URL + `", "plain": "` + plain.URL + `"}}`})
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true, Insecure: true})
	run := func(name string) (*Result, error) {
		req, err := r.Project.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		return r.Run(context.Background(), req)
	}
	for name, want := range map[string]string{"any": "HTTP/2.0", "one": "HTTP/1.1", "two": "HTTP/2.0"} {
		res, err := run(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Response.Proto != want || string(res.Raw().Body) != want {
			t.Fatalf("%s: proto %q body %q err %v; want %s", name, res.Response.Proto, res.Raw().Body, err, want)
		}
	}
	if _, err := run("two-refused"); ExitCode(err) != ExitTransport || !strings.Contains(err.Error(), "HTTP/2") {
		t.Fatalf("two-refused: %v", err)
	}
	if _, err := run("two-plain"); ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "h2c") {
		t.Fatalf("two-plain: %v", err)
	}
}
