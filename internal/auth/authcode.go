package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// The OAuth2 authorization code grant with PKCE (RFC 6749 section 4.1 and
// RFC 7636): the flow behind "sign in as yourself" for user-facing APIs.
// It needs a browser once: apic listens on a loopback port, sends the
// person to the provider's authorization URL, receives the code on the
// redirect, and exchanges it for a token, which is then cached and
// refreshed like the other grants. It is a human flow by design: under
// --json, in MCP or without a terminal it never starts, and the request
// fails with a message saying to run it once interactively.

// authCodeTimeout is how long apic waits for the browser to come back.
const authCodeTimeout = 5 * time.Minute

// ErrNotInteractive is returned when the grant needs a browser and no
// person is there to use one.
var ErrNotInteractive = errors.New("oauth2: grant=authorization_code needs a browser sign-in and no cached token exists; run the request once interactively (apic run <id>) to sign in, and the token is cached and refreshed from then on")

func authorizationCode(ctx context.Context, s *Spec, env *Env) (*tokenResponse, error) {
	if !env.Interactive {
		return nil, ErrNotInteractive
	}
	verifier, challenge, err := pkce()
	if err != nil {
		return nil, err
	}
	state, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	// Spec.check validated redirectPort and redirectUrl when the spec was
	// parsed. A redirectUrl is sent as written, since the provider matches
	// it exactly; apic listens on its port and path.
	port, _ := strconv.Atoi(s.Options["redirectPort"])
	path := "/callback"
	if u := s.Options["redirectUrl"]; u != "" {
		port, path, _ = redirectTarget(u)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("oauth2: listening for the redirect: %w", err)
	}
	defer func() { _ = ln.Close() }()
	redirectURI := "http://" + ln.Addr().String() + path
	if u := s.Options["redirectUrl"]; u != "" {
		redirectURI = u
	}

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {s.Options["clientId"]},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if sc := s.Options["scope"]; sc != "" {
		q.Set("scope", sc)
	}
	if a := s.Options["audience"]; a != "" {
		q.Set("audience", a)
	}
	authURL, err := url.Parse(s.Options["authUrl"])
	if err != nil {
		return nil, fmt.Errorf("oauth2: bad authUrl %q", s.Options["authUrl"])
	}
	existing := authURL.Query()
	for k, v := range q {
		existing[k] = v
	}
	authURL.RawQuery = existing.Encode()

	type outcome struct {
		code string
		err  error
	}
	done := make(chan outcome, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		params := r.URL.Query()
		var out outcome
		switch {
		case params.Get("state") != state:
			out.err = errors.New("oauth2: the redirect carried the wrong state; someone else may have sent it, so the sign-in was refused")
		case params.Get("error") != "":
			out.err = fmt.Errorf("oauth2: the provider refused: %s %s", params.Get("error"), params.Get("error_description"))
		case params.Get("code") == "":
			out.err = errors.New("oauth2: the redirect carried no code")
		default:
			out.code = params.Get("code")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if out.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "<!doctype html><title>apic</title><p>Sign-in failed: %s</p>", htmlEscape(out.err.Error()))
		} else {
			_, _ = fmt.Fprint(w, "<!doctype html><title>apic</title><p>Signed in. You can close this tab and go back to the terminal.</p>")
		}
		select {
		case done <- out:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	if env.Stderr != nil {
		fmt.Fprintf(env.Stderr, "\nTo sign in, open %s\n(the redirect URI is %s; register it with the provider if it asks)\nWaiting for the browser...", authURL, redirectURI)
	}
	opener := env.OpenBrowser
	if opener == nil {
		opener = OpenBrowser
	}
	// The URL is printed either way, so a machine without a browser can
	// still copy it somewhere that has one.
	_ = opener(authURL.String())

	timeout := authCodeTimeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var res outcome
	select {
	case <-ctx.Done():
		if env.Stderr != nil {
			fmt.Fprintln(env.Stderr)
		}
		return nil, fmt.Errorf("oauth2: no sign-in arrived within %s", timeout)
	case res = <-done:
	}
	if env.Stderr != nil {
		fmt.Fprintln(env.Stderr)
	}
	if res.err != nil {
		return nil, res.err
	}
	return tokenRequest(ctx, s, env, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {res.code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
}

// pkce returns a code verifier and its S256 challenge (RFC 7636).
func pkce() (verifier, challenge string, err error) {
	verifier, err = randomToken(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth2: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// OpenBrowser asks the system to open a URL: xdg-open, open or rundll32.
// Failure is not fatal; the URL is always printed as well.
func OpenBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}

// redirectTarget reads a redirectUrl: plain http on this machine
// (localhost, 127.0.0.1 or [::1]) with a port, since apic is the one
// listening. It returns the port and the path to answer on.
func redirectTarget(raw string) (port int, path string, err error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return 0, "", fmt.Errorf("redirectUrl %q must be an http:// URL on this machine", raw)
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return 0, "", fmt.Errorf("redirectUrl %q must point at localhost, where apic listens for the sign-in", raw)
	}
	port, err = strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return 0, "", fmt.Errorf("redirectUrl %q needs a port", raw)
	}
	path = u.Path
	if path == "" {
		path = "/"
	}
	return port, path, nil
}
