package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// cachedToken is what apic keeps in the session for oauth2 and exec.
type cachedToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func encodeToken(t cachedToken) string {
	b, _ := json.Marshal(t)
	return string(b)
}

func decodeToken(s string) (cachedToken, error) {
	var t cachedToken
	err := json.Unmarshal([]byte(s), &t)
	return t, err
}

// DescribeCached renders a cached token entry for `apic session`.
func DescribeCached(raw string, now time.Time) string {
	t, err := decodeToken(raw)
	if err != nil {
		return "(unreadable)"
	}
	left := t.ExpiresAt.Sub(now).Round(time.Second)
	if left <= 0 {
		return "expired"
	}
	return fmt.Sprintf("token, expires in %s", left)
}

// CacheKey identifies the token cache entry for a rendered oauth2 spec.
func CacheKey(s *Spec) string {
	o := s.Options
	return "$oauth2:" + hashKey(strings.Join([]string{
		o["tokenUrl"],
		o["deviceUrl"],
		o["clientId"],
		o["clientSecret"],
		o["clientAuth"],
		s.grant(),
		o["scope"],
		o["username"],
		o["password"],
		o["audience"],
	}, "\x00"))
}

const skew = 60 * time.Second

func oauth2Token(ctx context.Context, s *Spec, env *Env) (string, error) {
	key := CacheKey(s)
	var cached cachedToken
	if env.Cache != nil {
		if raw, ok := env.Cache.Get(key); ok {
			if c, err := decodeToken(raw); err == nil {
				cached = c
				if c.AccessToken != "" && c.ExpiresAt.After(env.now().Add(skew)) {
					return c.AccessToken, nil
				}
			}
		}
	}
	var tok *tokenResponse
	var err error
	if cached.RefreshToken != "" {
		tok, err = tokenRequest(ctx, s, env, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {cached.RefreshToken}})
		if err != nil {
			tok = nil // fall through to a fresh grant
		}
	}
	if tok == nil {
		switch s.grant() {
		case "client_credentials":
			form := url.Values{"grant_type": {"client_credentials"}}
			tok, err = tokenRequest(ctx, s, env, form)
		case "password":
			form := url.Values{"grant_type": {"password"}, "username": {s.Options["username"]}, "password": {s.Options["password"]}}
			tok, err = tokenRequest(ctx, s, env, form)
		case "device_code":
			tok, err = deviceCode(ctx, s, env)
		}
		if err != nil {
			return "", err
		}
	}
	entry := cachedToken{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, ExpiresAt: env.now().Add(time.Duration(tok.ExpiresIn) * time.Second)}
	if tok.ExpiresIn == 0 {
		entry.ExpiresAt = env.now().Add(time.Hour)
	}
	if entry.RefreshToken == "" {
		entry.RefreshToken = cached.RefreshToken
	}
	if env.Cache != nil {
		if err := env.Cache.Set(key, encodeToken(entry)); err != nil {
			return "", err
		}
	}
	return tok.AccessToken, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func tokenRequest(ctx context.Context, s *Spec, env *Env, form url.Values) (*tokenResponse, error) {
	if sc := s.Options["scope"]; sc != "" {
		form.Set("scope", sc)
	}
	if a := s.Options["audience"]; a != "" {
		form.Set("audience", a)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Options["tokenUrl"], strings.NewReader(""))
	if err != nil {
		return nil, fmt.Errorf("oauth2: %w", err)
	}
	if s.Options["clientAuth"] == "basic" {
		req.SetBasicAuth(s.Options["clientId"], s.Options["clientSecret"])
	} else {
		form.Set("client_id", s.Options["clientId"])
		if cs := s.Options["clientSecret"]; cs != "" {
			form.Set("client_secret", cs)
		}
	}
	req.Body = io.NopCloser(strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := env.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth2: token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	var tok tokenResponse
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("oauth2: %s returned %d with a non-JSON body: %s", s.Options["tokenUrl"], resp.StatusCode, truncate(string(data)))
	}
	if tok.Error != "" {
		return &tok, fmt.Errorf("oauth2: %s: %s %s", s.Options["tokenUrl"], tok.Error, tok.ErrorDesc)
	}
	if resp.StatusCode >= 300 || tok.AccessToken == "" {
		return nil, fmt.Errorf("oauth2: %s returned %d without an access_token: %s", s.Options["tokenUrl"], resp.StatusCode, truncate(string(data)))
	}
	return &tok, nil
}

type deviceResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	Error                   string `json:"error"`
}

func deviceCode(ctx context.Context, s *Spec, env *Env) (*tokenResponse, error) {
	form := url.Values{"client_id": {s.Options["clientId"]}}
	if sc := s.Options["scope"]; sc != "" {
		form.Set("scope", sc)
	}
	if a := s.Options["audience"]; a != "" {
		form.Set("audience", a)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Options["deviceUrl"], strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := env.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth2: device authorization: %w", err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var dev deviceResponse
	if err := json.Unmarshal(data, &dev); err != nil || dev.DeviceCode == "" {
		return nil, fmt.Errorf("oauth2: device authorization failed (%d): %s", resp.StatusCode, truncate(string(data)))
	}
	uri := dev.VerificationURIComplete
	if uri == "" {
		uri = dev.VerificationURI
	}
	if env.Stderr != nil {
		fmt.Fprintf(env.Stderr, "\nTo sign in, open %s and enter the code %s\nWaiting for approval", uri, dev.UserCode)
	}
	interval := time.Duration(dev.Interval) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	deadline := env.now().Add(time.Duration(dev.ExpiresIn) * time.Second)
	if dev.ExpiresIn == 0 {
		deadline = env.now().Add(5 * time.Minute)
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		if env.now().After(deadline) {
			return nil, fmt.Errorf("oauth2: device code expired before it was approved")
		}
		tok, err := tokenRequest(ctx, s, env, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {dev.DeviceCode},
		})
		if err == nil {
			if env.Stderr != nil {
				fmt.Fprintln(env.Stderr, " approved.")
			}
			return tok, nil
		}
		if tok != nil {
			switch tok.Error {
			case "authorization_pending":
				if env.Stderr != nil {
					fmt.Fprint(env.Stderr, ".")
				}
				continue
			case "slow_down":
				interval += 5 * time.Second
				continue
			}
		}
		return nil, err
	}
}

func (e *Env) client() *http.Client {
	if e.Client != nil {
		return e.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
