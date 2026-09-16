package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	good := map[string]string{
		"none":                        "none",
		"bearer {{token}}":            "bearer",
		"basic {{user}} {{password}}": "basic",
		"aws":                         "aws",
		"aws service=s3 region=eu-west-2 profile=dev":                                 "aws",
		"oauth2 tokenUrl={{tokenUrl}} clientId=a clientSecret=b scope=\"read write\"": "oauth2",
		"oauth2 grant=password tokenUrl=u clientId=a username=x password=y":           "oauth2",
		"oauth2 grant=device_code tokenUrl=u deviceUrl=d clientId=a":                  "oauth2",
		"exec gcloud auth print-access-token ttl=5m":                                  "exec",
		"exec az account get-access-token --query=accessToken header=X-Token prefix=": "exec",
	}
	for raw, typ := range good {
		s, err := Parse(raw)
		if err != nil || s.Type != typ {
			t.Errorf("%q: %v (%+v)", raw, err, s)
		}
	}
	s, _ := Parse("exec az account get-access-token --query=accessToken header=X-Token prefix=")
	if len(s.Args) != 4 || s.Args[3] != "--query=accessToken" || s.Options["header"] != "X-Token" || s.Options["prefix"] != "" {
		t.Errorf("exec parse: %+v", s)
	}
	s, _ = Parse(`oauth2 tokenUrl=u clientId=a scope="read write"`)
	if s.Options["scope"] != "read write" {
		t.Errorf("quoted option: %+v", s)
	}
	bad := []string{"", "magic", "bearer", "basic user", "aws sigv4", "aws foo=1", "oauth2 clientId=a", "oauth2 tokenUrl=u", "oauth2 tokenUrl=u clientId=a grant=implicit", "exec", "none x", `bearer "unterminated`}
	for _, raw := range bad {
		if _, err := Parse(raw); err == nil {
			t.Errorf("%q should fail", raw)
		}
	}
}

func TestBearerAndBasic(t *testing.T) {
	req := httptest.NewRequest("GET", "http://x/", nil)
	s, _ := Parse("bearer abc")
	if err := Apply(context.Background(), s, req, nil, &Env{}); err != nil || req.Header.Get("Authorization") != "Bearer abc" {
		t.Fatal(err, req.Header)
	}
	s, _ = Parse("basic alice s3cret")
	_ = Apply(context.Background(), s, req, nil, &Env{})
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	if req.Header.Get("Authorization") != want {
		t.Fatal(req.Header.Get("Authorization"))
	}
}

// Vectors from the AWS SigV4 test suite (aws-sig-v4-test-suite, 2015-08-30).
func TestSigV4TestVectors(t *testing.T) {
	creds := AWSCredentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}
	at := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)
	cases := []struct{ name, method, url, want string }{
		{"get-vanilla", "GET", "https://example.amazonaws.com/", "5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"},
		{"get-vanilla-query-order-key-case", "GET", "https://example.amazonaws.com/?Param2=value2&Param1=value1", "b97d918cfa904a5beff61c982a1b6f458b799221646efd99d3219ec94cdf2500"},
		{"get-vanilla-empty-query-key", "GET", "https://example.amazonaws.com/?Param1=value1", "a67d582fa61cc504c4bae71f336f98b97f1ea3c7a6bfe1b6e45aec72011b9aeb"},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, c.url, nil)
		// The suite signs only host and x-amz-date; our signer also adds
		// x-amz-content-sha256, so strip it to compare against the vectors.
		signNoContentHash(req, creds, "service", "us-east-1", at)
		got := req.Header.Get("Authorization")
		if !strings.HasSuffix(got, "Signature="+c.want) {
			t.Errorf("%s: %s", c.name, got)
		}
		if !strings.Contains(got, "SignedHeaders=host;x-amz-date,") {
			t.Errorf("%s: signed headers %s", c.name, got)
		}
	}
}

func TestAWSSignsThroughApply(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secretkey")
	t.Setenv("AWS_SESSION_TOKEN", "sess")
	t.Setenv("AWS_REGION", "eu-west-2")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent")

	var got http.Header
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()

	body := []byte(`{"a":1}`)
	req, _ := http.NewRequest("POST", srv.URL+"/orders?x=1", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	at := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	s, _ := Parse("aws service=execute-api")
	if err := Apply(context.Background(), s, req, body, &Env{Now: func() time.Time { return at }}); err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	authz := got.Get("Authorization")
	if !strings.HasPrefix(authz, "AWS4-HMAC-SHA256 Credential=AKIAEXAMPLE/20260912/eu-west-2/execute-api/aws4_request, SignedHeaders=content-length;content-type;host;x-amz-content-sha256;x-amz-date;x-amz-security-token, Signature=") {
		t.Fatalf("authorization: %s", authz)
	}
	if got.Get("X-Amz-Date") != "20260912T080000Z" || got.Get("X-Amz-Security-Token") != "sess" || string(gotBody) != string(body) {
		t.Fatalf("headers: %v body: %s", got, gotBody)
	}
}

func TestAWSProfileFilesAndCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(home, "credentials"))
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(home, "config"))
	must(t, os.WriteFile(filepath.Join(home, "credentials"), []byte("[default]\naws_access_key_id = DEF\naws_secret_access_key = defsecret\n\n[static]\naws_access_key_id = STAT\naws_secret_access_key = statsecret\naws_session_token = stattok\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(home, "config"), []byte("[default]\nregion = us-east-1\n\n[profile static]\nregion = eu-west-2\ns3 =\n    max_concurrent_requests = 20\n\n[profile sso]\nsso_start_url = https://x\nregion = ap-southeast-2\n"), 0o600))

	s, _ := Parse("aws")
	got, err := resolveAWS(context.Background(), s)
	if err != nil || got.Creds.AccessKeyID != "DEF" || got.Region != "us-east-1" || got.Source != "profile default" {
		t.Fatalf("default: %+v %v", got, err)
	}
	s, _ = Parse("aws profile=static")
	got, err = resolveAWS(context.Background(), s)
	if err != nil || got.Creds.AccessKeyID != "STAT" || got.Creds.SessionToken != "stattok" || got.Region != "eu-west-2" {
		t.Fatalf("static: %+v %v", got, err)
	}
	// An explicit profile wins over environment keys.
	t.Setenv("AWS_ACCESS_KEY_ID", "ENV")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "envsecret")
	got, _ = resolveAWS(context.Background(), s)
	if got.Creds.AccessKeyID != "STAT" {
		t.Fatalf("explicit profile should win: %+v", got)
	}
	s, _ = Parse("aws")
	got, _ = resolveAWS(context.Background(), s)
	if got.Creds.AccessKeyID != "ENV" || got.Source != "environment" {
		t.Fatalf("env: %+v", got)
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")

	// A profile without static keys falls back to the AWS CLI.
	if runtime.GOOS == "windows" {
		t.Skip("fake aws cli script needs a POSIX shell")
	}
	bin := t.TempDir()
	must(t, os.WriteFile(filepath.Join(bin, "aws"), []byte("#!/bin/sh\n[ \"$1 $2 $4\" = \"configure export-credentials sso\" ] || { echo wrong args >&2; exit 2; }\necho '{\"Version\":1,\"AccessKeyId\":\"CLI\",\"SecretAccessKey\":\"clisecret\",\"SessionToken\":\"clitok\"}'\n"), 0o755))
	t.Setenv("PATH", bin)
	s, _ = Parse("aws profile=sso")
	got, err = resolveAWS(context.Background(), s)
	if err != nil || got.Creds.AccessKeyID != "CLI" || got.Creds.SessionToken != "clitok" || got.Region != "ap-southeast-2" || !strings.HasPrefix(got.Source, "aws cli") {
		t.Fatalf("cli: %+v %v", got, err)
	}
	// And the CLI's error is surfaced.
	must(t, os.WriteFile(filepath.Join(bin, "aws"), []byte("#!/bin/sh\necho 'Error loading SSO Token: run aws sso login' >&2; exit 1\n"), 0o755))
	if _, err := resolveAWS(context.Background(), s); err == nil || !strings.Contains(err.Error(), "aws sso login") {
		t.Fatalf("want cli error, got %v", err)
	}
	// No CLI and no keys: a clear error.
	t.Setenv("PATH", t.TempDir())
	if _, err := resolveAWS(context.Background(), s); err == nil || !strings.Contains(err.Error(), `profile "sso"`) {
		t.Fatalf("want no-credentials error, got %v", err)
	}
}

func signNoContentHash(req *http.Request, creds AWSCredentials, service, region string, at time.Time) {
	SignSigV4(req, nil, creds, service, region, at)
	// Re-sign without x-amz-content-sha256 to match the official vectors.
	req.Header.Del("X-Amz-Content-Sha256")
	req.Header.Del("Authorization")
	signWithout(req, creds, service, region, at)
}

func TestAWSNeedsRegion(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "a")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "b")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_CONFIG_FILE", "/nonexistent")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent")
	req := httptest.NewRequest("GET", "http://x/", nil)
	s, _ := Parse("aws")
	if err := Apply(context.Background(), s, req, nil, &Env{}); err == nil || !strings.Contains(err.Error(), "region") {
		t.Fatalf("want region error, got %v", err)
	}
}

type memCache map[string]string

func (m memCache) Get(k string) (string, bool) { v, ok := m[k]; return v, ok }
func (m memCache) Set(k, v string) error       { m[k] = v; return nil }

type errCache struct{ err error }

func (c errCache) Get(string) (string, bool) { return "", false }
func (c errCache) Set(string, string) error  { return c.err }

func TestOAuth2ClientCredentialsCachingAndRefresh(t *testing.T) {
	var calls atomic.Int32
	var lastForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		lastForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		switch r.PostForm.Get("grant_type") {
		case "client_credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-1", "refresh_token": "ref-1", "expires_in": 3600})
		case "refresh_token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-2", "expires_in": 3600})
		default:
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "unsupported_grant_type"})
		}
	}))
	defer srv.Close()

	now := time.Date(2026, 9, 12, 8, 0, 0, 0, time.UTC)
	cache := memCache{}
	env := &Env{Cache: cache, Now: func() time.Time { return now }}
	s, _ := Parse("oauth2 tokenUrl=" + srv.URL + " clientId=cid clientSecret=csec scope=read audience=aud")

	req := httptest.NewRequest("GET", "http://x/", nil)
	if err := Apply(context.Background(), s, req, nil, env); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "Bearer tok-1" || calls.Load() != 1 {
		t.Fatalf("first: %s calls=%d", req.Header.Get("Authorization"), calls.Load())
	}
	if lastForm.Get("client_id") != "cid" || lastForm.Get("client_secret") != "csec" || lastForm.Get("scope") != "read" || lastForm.Get("audience") != "aud" {
		t.Fatalf("form: %v", lastForm)
	}

	// Second call within the lifetime: served from cache.
	req = httptest.NewRequest("GET", "http://x/", nil)
	_ = Apply(context.Background(), s, req, nil, env)
	if calls.Load() != 1 || req.Header.Get("Authorization") != "Bearer tok-1" {
		t.Fatalf("cache miss: calls=%d", calls.Load())
	}

	// Near expiry: refreshed with the refresh token.
	now = now.Add(3600*time.Second - 30*time.Second)
	req = httptest.NewRequest("GET", "http://x/", nil)
	_ = Apply(context.Background(), s, req, nil, env)
	if calls.Load() != 2 || req.Header.Get("Authorization") != "Bearer tok-2" || lastForm.Get("grant_type") != "refresh_token" {
		t.Fatalf("refresh: calls=%d authz=%s form=%v", calls.Load(), req.Header.Get("Authorization"), lastForm)
	}
	if !strings.Contains(DescribeCached(cache[CacheKey(s)], now), "expires in") {
		t.Fatal(DescribeCached(cache[CacheKey(s)], now))
	}
}

func TestOAuth2BasicClientAuthAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		if !ok || u != "cid" || p != "csec" {
			w.WriteHeader(401)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_client", "error_description": "bad secret"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 60})
	}))
	defer srv.Close()
	req := httptest.NewRequest("GET", "http://x/", nil)
	s, _ := Parse("oauth2 tokenUrl=" + srv.URL + " clientId=cid clientSecret=csec clientAuth=basic")
	if err := Apply(context.Background(), s, req, nil, &Env{}); err != nil || req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatal(err)
	}
	s, _ = Parse("oauth2 tokenUrl=" + srv.URL + " clientId=cid clientSecret=wrong clientAuth=basic")
	if err := Apply(context.Background(), s, req, nil, &Env{}); err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("want invalid_client, got %v", err)
	}
}

func TestOAuth2ClientAuthValidation(t *testing.T) {
	if _, err := Parse("oauth2 tokenUrl=https://idp/token clientId=cid clientAuth=basci"); err == nil || !strings.Contains(err.Error(), "clientAuth must be body or basic") {
		t.Fatalf("want clientAuth validation error, got %v", err)
	}
}

func TestOAuth2CacheKeyIncludesCredentialInputs(t *testing.T) {
	base, _ := Parse("oauth2 tokenUrl=https://idp/token clientId=cid clientSecret=secret clientAuth=basic scope=read audience=aud")
	diffSecret, _ := Parse("oauth2 tokenUrl=https://idp/token clientId=cid clientSecret=other clientAuth=basic scope=read audience=aud")
	diffClientAuth, _ := Parse("oauth2 tokenUrl=https://idp/token clientId=cid clientSecret=secret clientAuth=body scope=read audience=aud")
	passwordA := &Spec{Type: "oauth2", Options: map[string]string{
		"tokenUrl": "https://idp/token",
		"clientId": "cid",
		"grant":    "password",
		"username": "alice",
		"password": "pw-one",
	}}
	passwordB := &Spec{Type: "oauth2", Options: map[string]string{
		"tokenUrl": "https://idp/token",
		"clientId": "cid",
		"grant":    "password",
		"username": "alice",
		"password": "pw-two",
	}}

	if CacheKey(base) == CacheKey(diffSecret) {
		t.Fatal("clientSecret must affect cache key")
	}
	if CacheKey(base) == CacheKey(diffClientAuth) {
		t.Fatal("clientAuth must affect cache key")
	}
	if CacheKey(passwordA) == CacheKey(passwordB) {
		t.Fatal("password must affect cache key")
	}
}

func TestOAuth2CacheSaveErrorReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 60})
	}))
	defer srv.Close()

	s, _ := Parse("oauth2 tokenUrl=" + srv.URL + " clientId=cid")
	req := httptest.NewRequest("GET", "http://x/", nil)
	errWant := errors.New("save failed")
	if err := Apply(context.Background(), s, req, nil, &Env{Cache: errCache{err: errWant}}); !errors.Is(err, errWant) {
		t.Fatalf("want cache save error, got %v", err)
	}
}

func TestOAuth2DeviceCode(t *testing.T) {
	var polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/device", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "dev", "user_code": "ABCD-1234", "verification_uri": "https://idp/activate", "expires_in": 60, "interval": 1})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("device_code") != "dev" {
			w.WriteHeader(400)
			return
		}
		if polls.Add(1) < 2 {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "dtok", "expires_in": 60})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	var prompt strings.Builder
	req := httptest.NewRequest("GET", "http://x/", nil)
	s, _ := Parse("oauth2 grant=device_code tokenUrl=" + srv.URL + "/token deviceUrl=" + srv.URL + "/device clientId=cid")
	if err := Apply(context.Background(), s, req, nil, &Env{Stderr: &prompt}); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "Bearer dtok" || polls.Load() != 2 {
		t.Fatalf("authz=%s polls=%d", req.Header.Get("Authorization"), polls.Load())
	}
	if !strings.Contains(prompt.String(), "ABCD-1234") || !strings.Contains(prompt.String(), "https://idp/activate") {
		t.Fatalf("prompt: %q", prompt.String())
	}
}

func TestExec(t *testing.T) {
	req := httptest.NewRequest("GET", "http://x/", nil)
	s, _ := Parse("exec go env GOOS")
	if err := Apply(context.Background(), s, req, nil, &Env{}); err == nil || !strings.Contains(err.Error(), "allowExec") {
		t.Fatalf("want disabled error, got %v", err)
	}
	if err := Apply(context.Background(), s, req, nil, &Env{AllowExec: true}); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") || len(got) < 8 {
		t.Fatalf("authz %q", got)
	}
	s, _ = Parse("exec go env GOOS header=X-Api-Key prefix= ttl=1h")
	cache := memCache{}
	now := time.Now()
	env := &Env{AllowExec: true, Cache: cache, Now: func() time.Time { return now }}
	_ = Apply(context.Background(), s, req, nil, env)
	if got := req.Header.Get("X-Api-Key"); strings.Contains(got, " ") || got == "" {
		t.Fatalf("x-api-key %q", got)
	}
	if len(cache) != 1 {
		t.Fatal("ttl should cache")
	}
	s, _ = Parse("exec definitely-not-a-command-xyz")
	if err := Apply(context.Background(), s, req, nil, &Env{AllowExec: true}); err == nil {
		t.Fatal("want exec failure")
	}
}

func TestExecCacheSaveErrorReturned(t *testing.T) {
	s, _ := Parse("exec go env GOOS ttl=1h")
	req := httptest.NewRequest("GET", "http://x/", nil)
	errWant := errors.New("save failed")
	if err := Apply(context.Background(), s, req, nil, &Env{AllowExec: true, Cache: errCache{err: errWant}}); !errors.Is(err, errWant) {
		t.Fatalf("want cache save error, got %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
