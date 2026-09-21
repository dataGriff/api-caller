package runner

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dataGriff/api-caller/internal/project"
)

// proxyServer is an HTTP proxy that answers every absolute-URI request
// itself, recording what it was asked for, so a test can tell whether a
// request went through it without a second server behind it.
func proxyServer(t *testing.T, name string) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.String()+" auth="+r.Header.Get("Proxy-Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"via":"` + name + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

const proxyHTTP = `
### Ping
# @name ping
# @assert status == 200
GET {{baseUrl}}/ping
`

func proxyProject(t *testing.T, baseURL, yaml string) string {
	t.Helper()
	files := map[string]string{
		"api.http":             proxyHTTP,
		"http-client.env.json": `{"dev": {"baseUrl": "` + baseURL + `"}}`,
	}
	if yaml != "" {
		files["apic.yaml"] = yaml
	}
	return writeProject(t, files)
}

func runPing(t *testing.T, r *Runner) *Result {
	t.Helper()
	req, err := r.Project.Lookup("ping")
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func via(t *testing.T, res *Result) string {
	t.Helper()
	raw, _ := res.Response.Body.(json.RawMessage)
	var body map[string]string
	_ = json.Unmarshal(raw, &body)
	return body["via"]
}

func TestProxyPrecedenceFlagConfigEnvironment(t *testing.T) {
	flagProxy, flagSeen := proxyServer(t, "flag")
	confProxy, confSeen := proxyServer(t, "config")
	envProxy, envSeen := proxyServer(t, "env")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"via":"direct"}`))
	}))
	t.Cleanup(target.Close)
	// The target is on 127.0.0.1, which the environment's proxy would skip
	// the way every client does; give it a name the proxy answers for
	// instead, since the proxies never dial anything.
	t.Setenv("HTTP_PROXY", envProxy.URL)
	t.Setenv("NO_PROXY", "")

	dir := proxyProject(t, "http://api.example.test", "proxy: "+confProxy.URL+"\n")
	r := newRunner(t, dir, Options{Env: "dev", Proxy: flagProxy.URL})
	if got := via(t, runPing(t, r)); got != "flag" {
		t.Fatalf("--proxy should win, went via %q", got)
	}
	if len(*flagSeen) != 1 || !strings.HasPrefix((*flagSeen)[0], "GET http://api.example.test/ping") {
		t.Fatalf("proxy saw %v", *flagSeen)
	}

	r = newRunner(t, dir, Options{Env: "dev"})
	if got := via(t, runPing(t, r)); got != "config" {
		t.Fatalf("apic.yaml proxy should beat the environment, went via %q", got)
	}

	dir = proxyProject(t, "http://api.example.test", "")
	r = newRunner(t, dir, Options{Env: "dev"})
	if got := via(t, runPing(t, r)); got != "env" {
		t.Fatalf("HTTP_PROXY should apply without flag or config, went via %q", got)
	}
	if len(*confSeen) != 1 || len(*envSeen) != 1 {
		t.Fatalf("config proxy saw %d, env proxy saw %d", len(*confSeen), len(*envSeen))
	}

	// --no-proxy ignores all three and goes straight to the target.
	dir = proxyProject(t, target.URL, "proxy: "+confProxy.URL+"\n")
	r = newRunner(t, dir, Options{Env: "dev", Proxy: flagProxy.URL, NoProxy: true})
	if got := via(t, runPing(t, r)); got != "direct" {
		t.Fatalf("--no-proxy should go direct, went via %q", got)
	}
	if len(*flagSeen) != 1 || len(*confSeen) != 1 {
		t.Fatal("--no-proxy still touched a proxy")
	}
}

func TestProxyNoProxyListAndReporting(t *testing.T) {
	proxy, seen := proxyServer(t, "proxy")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"via":"direct"}`))
	}))
	t.Cleanup(target.Close)
	host := strings.TrimPrefix(target.URL, "http://")
	pu, _ := url.Parse(proxy.URL)
	withAuth := "http://alice:s3cret@" + pu.Host
	dir := proxyProject(t, target.URL, "proxy: "+withAuth+"\nnoProxy: [\".example.test\", \""+host+"\"]\n")
	r := newRunner(t, dir, Options{Env: "dev"})

	// The target is on the noProxy list, so the request goes direct and
	// describe says so.
	if got := via(t, runPing(t, r)); got != "direct" {
		t.Fatalf("noProxy host should go direct, went via %q", got)
	}
	if len(*seen) != 0 {
		t.Fatalf("proxy saw %v", *seen)
	}
	req, _ := r.Project.Lookup("ping")
	d := r.Describe(req)
	if d.Proxy == nil || !d.Proxy.Off || d.Proxy.Source != "noProxy" || d.Proxy.String() != "none (noProxy)" {
		t.Fatalf("describe proxy = %+v", d.Proxy)
	}

	// Another host goes through it, with the credentials sent to the
	// proxy and masked everywhere apic shows the setting.
	info := r.ProxyInfo("http://other.example.org/x")
	if info == nil || info.Off || info.Source != "apic.yaml" || info.URL != "http://***@"+pu.Host {
		t.Fatalf("ProxyInfo = %+v", info)
	}
	if info.Raw().String() != withAuth {
		t.Fatalf("raw proxy = %s", info.Raw())
	}
	dir = proxyProject(t, "http://other.example.org", "proxy: "+withAuth+"\n")
	r = newRunner(t, dir, Options{Env: "dev"})
	res := runPing(t, r)
	if via(t, res) != "proxy" || len(*seen) != 1 || !strings.Contains((*seen)[0], "auth=Basic ") {
		t.Fatalf("proxy saw %v", *seen)
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "s3cret") || !strings.Contains(string(data), `"proxy":{"url":"http://***@`+pu.Host+`","source":"apic.yaml"}`) {
		t.Fatalf("result JSON should carry the masked proxy:\n%s", data)
	}
}

func TestProxyRejectsBadURLs(t *testing.T) {
	dir := proxyProject(t, "http://api.example.test", "proxy: ftp://proxy.internal:21\n")
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(p, Options{Env: "dev"}); err == nil || ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "apic.yaml: proxy scheme") {
		t.Fatalf("err = %v", err)
	}
	if _, err := New(p, Options{Env: "dev", Proxy: "http://"}); err == nil || !strings.Contains(err.Error(), "--proxy: bad proxy URL") {
		t.Fatalf("err = %v", err)
	}
	// A bare host:port is an HTTP proxy, as for curl -x.
	r, err := New(p, Options{Env: "dev", Proxy: "proxy.internal:3128"})
	if err != nil {
		t.Fatal(err)
	}
	if info := r.ProxyInfo(""); info == nil || info.URL != "http://proxy.internal:3128" || info.Source != "--proxy" {
		t.Fatalf("ProxyInfo = %+v", info)
	}
}

func TestEnvProxySkipsLoopbackAndHonoursNoProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.internal:3128")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("NO_PROXY", ".internal, 10.0.0.0/8")
	cases := map[string]string{
		"https://api.example.com/x":   "http://proxy.internal:3128 (HTTPS_PROXY)",
		"https://localhost:8443/x":    "none (noProxy)",
		"https://127.0.0.1/x":         "none (noProxy)",
		"https://db.internal/x":       "none (noProxy)",
		"https://10.1.2.3/x":          "none (noProxy)",
		"http://api.example.com/x":    "",
		"https://api.example.com:443": "http://proxy.internal:3128 (HTTPS_PROXY)",
	}
	for raw, want := range cases {
		u, _ := url.Parse(raw)
		if got := envProxy(u).String(); got != want {
			t.Errorf("%s: got %q, want %q", raw, got, want)
		}
	}
}

func TestBadProxyVariableRefusesTheRequest(t *testing.T) {
	t.Setenv("HTTP_PROXY", "ftp://proxy.internal:21")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"via":"direct"}`))
	}))
	t.Cleanup(target.Close)
	// Loopback would skip the proxy; a name goes through it, and the
	// broken setting must not become a silent direct connection.
	dir := proxyProject(t, "http://api.example.test", "")
	r := newRunner(t, dir, Options{Env: "dev"})
	req, _ := r.Project.Lookup("ping")
	_, err := r.Run(context.Background(), req)
	if err == nil || ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "HTTP_PROXY: proxy scheme \"ftp\"") {
		t.Fatalf("err = %v", err)
	}
	if info := r.Describe(req).Proxy; info == nil || info.Error == "" || !strings.HasPrefix(info.String(), "invalid (HTTP_PROXY): ") {
		t.Fatalf("describe proxy = %+v", info)
	}
}

func TestTraceHooksAreSafeToCallConcurrently(t *testing.T) {
	var tm traceTimes
	tr := tm.trace()
	tm.mu.Lock()
	tm.start = time.Now()
	tm.mu.Unlock()
	var wg sync.WaitGroup
	// Two dials racing, as for a dual-stack host, plus DNS and TLS.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tr.DNSStart(httptrace.DNSStartInfo{})
			tr.DNSDone(httptrace.DNSDoneInfo{})
			tr.ConnectStart("tcp", "h")
			if i == 0 {
				tr.ConnectDone("tcp", "h", nil)
				tr.TLSHandshakeStart()
				tr.TLSHandshakeDone(tls.ConnectionState{}, nil)
				tr.GotConn(httptrace.GotConnInfo{})
				tr.GotFirstResponseByte()
			} else {
				tr.ConnectDone("tcp", "h", errors.New("lost the race"))
			}
		}(i)
	}
	wg.Wait()
	got := tm.timings(5 * time.Millisecond)
	if got.Reused || got.TotalMs != 5 || got.ConnectMs < 0 || got.TTFBMs > got.TotalMs {
		t.Fatalf("timings = %+v", got)
	}
}

func TestNoProxyMatch(t *testing.T) {
	list := []string{"localhost", ".corp.example", "*.svc", "10.0.0.0/8", "db.internal:5432", "EXACT.Host"}
	cases := map[string]bool{
		"http://localhost/":         true,
		"https://a.corp.example/":   true,
		"https://corp.example/":     true,
		"https://a.b.svc/":          true,
		"http://10.20.30.40/":       true,
		"http://11.0.0.1/":          false,
		"http://db.internal:5432/":  true,
		"http://db.internal:5433/":  false,
		"http://exact.host/":        true,
		"http://notexact.host/":     false,
		"https://api.example.com/":  false,
		"https://corp.example.com/": false,
		"https://xcorp.example/":    false,
		"http://db.internal/":       false,
		"http://DB.INTERNAL:5432/":  true,
	}
	for raw, want := range cases {
		u, _ := url.Parse(raw)
		if got := noProxyMatch(list, u); got != want {
			t.Errorf("%s: got %v, want %v", raw, got, want)
		}
	}
	if !noProxyMatch([]string{"*"}, &url.URL{Scheme: "https", Host: "anything"}) {
		t.Error("* should match everything")
	}
}
