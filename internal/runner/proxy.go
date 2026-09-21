package runner

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// ProxyInfo is the proxy in effect for one request, for `describe`,
// `env --json`, the verbose run output and `apic curl`.
type ProxyInfo struct {
	// URL is the proxy with any userinfo replaced by ***. Empty when Off.
	URL string `json:"url,omitempty"`
	// Source is where the setting came from: "--proxy", "apic.yaml", the
	// environment variable that named it, "--no-proxy", or "noProxy" when
	// the host is excluded by apic.yaml's noProxy list (or NO_PROXY for a
	// proxy from the environment).
	Source string `json:"source"`
	// Off means no proxy will be used although one is configured.
	Off bool `json:"off,omitempty"`
	// Error says the setting could not be used; the request is refused
	// with it rather than sent directly.
	Error string `json:"error,omitempty"`

	raw *url.URL // the proxy as it will be dialled; nil when Off
}

// String renders the proxy the way describe and -v show it.
func (p *ProxyInfo) String() string {
	if p == nil {
		return ""
	}
	if p.Error != "" {
		return "invalid (" + p.Source + "): " + p.Error
	}
	if p.Off {
		return "none (" + p.Source + ")"
	}
	return p.URL + " (" + p.Source + ")"
}

// Raw returns the proxy URL as it will be dialled, credentials included,
// for a curl export that must stay runnable. It is nil when Off.
func (p *ProxyInfo) Raw() *url.URL {
	if p == nil {
		return nil
	}
	return p.raw
}

// proxySettings is the explicit proxy from --proxy or apic.yaml, parsed
// once at New; nil means the environment decides.
type proxySettings struct {
	url     *url.URL
	source  string
	noProxy []string
}

// parseProxy validates a --proxy or apic.yaml proxy URL. A bare host:port is
// taken as an HTTP proxy, the way curl reads -x.
func parseProxy(raw, source string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, usagef("%s: bad proxy URL %q", source, raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, usagef("%s: proxy scheme %q not supported (http, https, socks5 or socks5h)", source, u.Scheme)
	}
	return u, nil
}

// proxyFunc is the Transport.Proxy for this runner: nothing under
// --no-proxy, the explicit proxy minus its noProxy hosts, else the
// HTTP_PROXY, HTTPS_PROXY and NO_PROXY variables.
func (r *Runner) proxyFunc() func(*http.Request) (*url.URL, error) {
	if r.Opts.NoProxy {
		return nil
	}
	return func(req *http.Request) (*url.URL, error) {
		info := r.proxyInfo(req.URL)
		if info == nil || info.Off {
			return nil, nil
		}
		if info.Error != "" {
			return nil, errors.New(info.Error)
		}
		return info.raw, nil
	}
}

// ProxyInfo reports the proxy in effect for a request URL. It is nil when
// no proxy is configured anywhere, so the field stays out of the output
// in the common case. An empty or unparsable URL reports the setting for
// an https request, which is what `apic env` wants.
func (r *Runner) ProxyInfo(rawURL string) *ProxyInfo {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		u = &url.URL{Scheme: "https", Host: "example.com"}
	}
	return r.proxyInfo(u)
}

func (r *Runner) proxyInfo(u *url.URL) *ProxyInfo {
	if r.Opts.NoProxy {
		return &ProxyInfo{Source: "--no-proxy", Off: true}
	}
	if s := r.proxy; s != nil {
		if noProxyMatch(s.noProxy, u) {
			return &ProxyInfo{Source: "noProxy", Off: true}
		}
		return &ProxyInfo{URL: maskUserinfo(s.url), Source: s.source, raw: s.url}
	}
	return envProxy(u)
}

// envProxy reads the proxy variables the way curl and Go do, by request
// scheme, without net/http's process-wide cache of them so a change in
// the environment is seen (and so tests can set them).
func envProxy(u *url.URL) *ProxyInfo {
	names := []string{"HTTP_PROXY", "http_proxy"}
	if strings.EqualFold(u.Scheme, "https") {
		names = []string{"HTTPS_PROXY", "https_proxy"}
	}
	for _, name := range names {
		v := os.Getenv(name)
		if v == "" {
			continue
		}
		p, err := parseProxy(v, name)
		if err != nil {
			return &ProxyInfo{Source: name, Error: err.Error()}
		}
		// The environment's proxy never applies to the local machine,
		// which is what every other client does with these variables.
		host := u.Hostname()
		if host == "localhost" || strings.HasSuffix(host, ".localhost") {
			return &ProxyInfo{Source: "noProxy", Off: true}
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return &ProxyInfo{Source: "noProxy", Off: true}
		}
		noProxy := os.Getenv("NO_PROXY")
		if noProxy == "" {
			noProxy = os.Getenv("no_proxy")
		}
		if noProxyMatch(strings.Split(noProxy, ","), u) {
			return &ProxyInfo{Source: "noProxy", Off: true}
		}
		return &ProxyInfo{URL: maskUserinfo(p), Source: name, raw: p}
	}
	return nil
}

// noProxyMatch reports whether a request host is excluded by a noProxy
// list: `*` for everything, `host`, `host:port`, `.suffix` or `*.suffix`
// for a domain and everything under it, an IP, or a CIDR.
func noProxyMatch(list []string, u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		if strings.EqualFold(u.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	ip := net.ParseIP(host)
	for _, entry := range list {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == "*" {
			return true
		}
		if ip != nil {
			if _, cidr, err := net.ParseCIDR(entry); err == nil && cidr.Contains(ip) {
				return true
			}
		}
		if h, p, err := net.SplitHostPort(entry); err == nil {
			if h == host && p == port {
				return true
			}
			continue
		}
		suffix := strings.TrimPrefix(strings.TrimPrefix(entry, "*"), ".")
		if entry == host || (suffix != "" && (host == suffix || strings.HasSuffix(host, "."+suffix))) {
			return true
		}
	}
	return false
}

// maskUserinfo renders a proxy URL with its credentials hidden, so the
// proxy can be shown wherever the request is.
func maskUserinfo(u *url.URL) string {
	if u.User == nil {
		return u.String()
	}
	c := *u
	c.User = nil
	// Built by hand: url.Userinfo would percent-encode the asterisks.
	return strings.Replace(c.String(), "://", "://"+Masked+"@", 1)
}

// String on a proxy source pair, for error messages.
func proxySource(flag bool) string {
	if flag {
		return "--proxy"
	}
	return "apic.yaml"
}
