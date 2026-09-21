package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// authServer plays an authorization server: /authorize checks the PKCE
// challenge and redirects the "browser" back with a code, /token checks
// the verifier against it and issues a token with a refresh token.
func authServer(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var authorizes, tokens atomic.Int32
	challenges := map[string]string{} // code -> challenge
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		authorizes.Add(1)
		q := r.URL.Query()
		if q.Get("response_type") != "code" || q.Get("client_id") != "cid" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" || q.Get("scope") != "openid profile" {
			http.Error(w, "bad authorize request: "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		redirect, err := url.Parse(q.Get("redirect_uri"))
		if err != nil || !strings.HasPrefix(redirect.String(), "http://127.0.0.1:") || redirect.Path != "/callback" {
			http.Error(w, "bad redirect_uri", http.StatusBadRequest)
			return
		}
		code := "code-" + q.Get("state")[:4]
		challenges[code] = q.Get("code_challenge")
		back := redirect.Query()
		back.Set("code", code)
		back.Set("state", q.Get("state"))
		redirect.RawQuery = back.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokens.Add(1)
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if challenges[r.PostForm.Get("code")] != base64.RawURLEncoding.EncodeToString(sum[:]) || r.PostForm.Get("client_id") != "cid" || !strings.HasPrefix(r.PostForm.Get("redirect_uri"), "http://127.0.0.1:") {
				w.WriteHeader(400)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "verifier or code"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 3600})
		case "refresh_token":
			if r.PostForm.Get("refresh_token") != "rt-1" {
				w.WriteHeader(400)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-2", "expires_in": 3600})
		default:
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "unsupported_grant_type"})
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &authorizes, &tokens
}

// browser visits the authorization URL the way a browser would, following
// the redirect back to apic's loopback listener.
func browser(t *testing.T) func(string) error {
	return func(u string) error {
		t.Helper()
		resp, err := http.Get(u) //nolint:noctx // test browser
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != 200 {
			t.Errorf("browser ended on %d at %s", resp.StatusCode, resp.Request.URL)
		}
		return nil
	}
}

func TestAuthorizationCodeRoundTripCacheAndRefresh(t *testing.T) {
	srv, authorizes, tokens := authServer(t)
	s, err := Parse(`oauth2 grant=authorization_code authUrl=` + srv.URL + `/authorize tokenUrl=` + srv.URL + `/token clientId=cid scope="openid profile"`)
	if err != nil {
		t.Fatal(err)
	}
	cache := memCache{}
	var prompt strings.Builder
	now := time.Now()
	env := &Env{Cache: cache, Stderr: &prompt, Interactive: true, OpenBrowser: browser(t), Now: func() time.Time { return now }}

	req := httptest.NewRequest("GET", "http://x/", nil)
	if err := Apply(context.Background(), s, req, nil, env); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "Bearer at-1" || authorizes.Load() != 1 || tokens.Load() != 1 {
		t.Fatalf("authz=%s authorizes=%d tokens=%d", req.Header.Get("Authorization"), authorizes.Load(), tokens.Load())
	}
	if !strings.Contains(prompt.String(), "To sign in, open "+srv.URL+"/authorize?") || !strings.Contains(prompt.String(), "redirect URI is http://127.0.0.1:") {
		t.Fatalf("prompt: %q", prompt.String())
	}
	// Cached: no browser, no token request.
	req = httptest.NewRequest("GET", "http://x/", nil)
	env.OpenBrowser = func(string) error { t.Fatal("browser opened with a cached token"); return nil }
	env.Interactive = false
	if err := Apply(context.Background(), s, req, nil, env); err != nil || req.Header.Get("Authorization") != "Bearer at-1" {
		t.Fatalf("cached: %v %s", err, req.Header.Get("Authorization"))
	}
	// Expired: refreshed with the refresh token, still no browser, even
	// when nobody is at the terminal.
	now = now.Add(2 * time.Hour)
	req = httptest.NewRequest("GET", "http://x/", nil)
	if err := Apply(context.Background(), s, req, nil, env); err != nil || req.Header.Get("Authorization") != "Bearer at-2" {
		t.Fatalf("refresh: %v %s", err, req.Header.Get("Authorization"))
	}
	if authorizes.Load() != 1 || tokens.Load() != 2 {
		t.Fatalf("after refresh: authorizes=%d tokens=%d", authorizes.Load(), tokens.Load())
	}
	// The cache entry is described, never printed.
	for k, v := range cache {
		if !strings.HasPrefix(k, "$oauth2:") || strings.Contains(DescribeCached(v, now), "at-2") {
			t.Fatalf("cache %s = %s", k, DescribeCached(v, now))
		}
	}
}

func TestAuthorizationCodeRefusesWithoutATerminal(t *testing.T) {
	srv, authorizes, _ := authServer(t)
	s, _ := Parse("oauth2 grant=authorization_code authUrl=" + srv.URL + "/authorize tokenUrl=" + srv.URL + "/token clientId=cid")
	req := httptest.NewRequest("GET", "http://x/", nil)
	err := Apply(context.Background(), s, req, nil, &Env{Cache: memCache{}, OpenBrowser: func(string) error { t.Fatal("browser opened"); return nil }})
	if !errors.Is(err, ErrNotInteractive) || !strings.Contains(err.Error(), "interactively") {
		t.Fatalf("err = %v", err)
	}
	if authorizes.Load() != 0 || req.Header.Get("Authorization") != "" {
		t.Fatal("nothing should have been sent")
	}
}

func TestAuthorizationCodeRejectsWrongState(t *testing.T) {
	srv, _, tokens := authServer(t)
	s, _ := Parse("oauth2 grant=authorization_code authUrl=" + srv.URL + "/authorize tokenUrl=" + srv.URL + "/token clientId=cid")
	// A "browser" that comes back with somebody else's state.
	forged := func(u string) error {
		au, _ := url.Parse(u)
		cb := au.Query().Get("redirect_uri") + "?code=stolen&state=not-ours"
		resp, err := http.Get(cb) //nolint:noctx // test
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("forged callback answered %d", resp.StatusCode)
		}
		return nil
	}
	req := httptest.NewRequest("GET", "http://x/", nil)
	err := Apply(context.Background(), s, req, nil, &Env{Interactive: true, OpenBrowser: forged, Stderr: &strings.Builder{}})
	if err == nil || !strings.Contains(err.Error(), "wrong state") || tokens.Load() != 0 {
		t.Fatalf("err=%v tokens=%d", err, tokens.Load())
	}
	// And the provider saying no.
	refused := func(u string) error {
		au, _ := url.Parse(u)
		cb := au.Query().Get("redirect_uri") + "?error=access_denied&error_description=nope&state=" + au.Query().Get("state")
		resp, err := http.Get(cb) //nolint:noctx // test
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		return nil
	}
	err = Apply(context.Background(), s, req, nil, &Env{Interactive: true, OpenBrowser: refused, Stderr: &strings.Builder{}})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err=%v", err)
	}
}

func TestAuthorizationCodeParse(t *testing.T) {
	for _, bad := range []string{
		"oauth2 grant=authorization_code tokenUrl=t clientId=c",
		"oauth2 grant=authorization_code tokenUrl=t clientId=c authUrl=a redirectPort=abc",
		"oauth2 grant=authorization_code tokenUrl=t clientId=c authUrl=a redirectPort=70000",
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
	s, err := Parse("oauth2 grant=authorization_code tokenUrl=t clientId=c authUrl=a redirectPort=8976 clientSecret=s")
	if err != nil || s.Options["redirectPort"] != "8976" {
		t.Fatalf("%v %+v", err, s)
	}
	// authUrl is part of the cache key, so two providers never share a token.
	other, _ := Parse("oauth2 grant=authorization_code tokenUrl=t clientId=c authUrl=b redirectPort=8976 clientSecret=s")
	if CacheKey(s) == CacheKey(other) {
		t.Fatal("cache key ignores authUrl")
	}
	if _, _, err := pkce(); err != nil {
		t.Fatal(err)
	}
}
