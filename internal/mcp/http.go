package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The streamable HTTP transport, for an agent that is not on the same
// machine: a hosted agent, a shared development box, or a container that
// also runs the API under test. stdio stays the default. The server binds
// the loopback interface unless a bearer token guards it, because an MCP
// server runs the project's requests with the project's credentials and
// must not be reachable by whoever can route a packet to the host.

// CheckBind refuses an address outside the loopback interface unless a
// token guards the server.
func CheckBind(addr, token string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--http: %q is not host:port", addr)
	}
	if token != "" {
		return nil
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("--http %s binds outside the loopback interface; set --token (or APIC_MCP_TOKEN) so only your agent can call it", addr)
}

// Handler serves the project over the MCP streamable HTTP transport,
// behind a bearer token when one is given.
func Handler(cfg Config, token string) (http.Handler, error) {
	srv, err := New(cfg)
	if err != nil {
		return nil, err
	}
	var h http.Handler = sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, nil)
	if token != "" {
		h = requireToken(token, h)
	}
	return h, nil
}

// requireToken answers 401 to every request without the bearer token.
func requireToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="apic mcp"`)
			http.Error(w, "unauthorized: this apic mcp server needs its bearer token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ServeHTTP listens on addr and serves until ctx ends. ready, when set, is
// called with the bound address once the listener is up.
func ServeHTTP(ctx context.Context, cfg Config, addr, token string, ready func(string)) error {
	if err := CheckBind(addr, token); err != nil {
		return err
	}
	h, err := Handler(cfg, token)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if ready != nil {
		ready(ln.Addr().String())
	}
	hs := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(shutdown)
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
