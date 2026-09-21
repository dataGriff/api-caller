package auth

import (
	"crypto/md5" //nolint:gosec // RFC 7616 names MD5 as an algorithm; servers still offer it
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
	"sync"
)

// This file is HTTP Digest authentication (RFC 7616, which obsoletes RFC
// 2617), written from the RFC in the style of sigv4.go: no dependency.
// The server challenges with WWW-Authenticate: Digest and the client
// answers with a hash over the credentials, the nonce and the request,
// so the password never travels. apic answers the challenge inside one
// run of a request, and remembers the challenge so later requests to the
// same server in the same invocation authenticate on the first try, with
// the nonce count going up as the RFC asks.

// digestChallenge is one parsed `Digest` challenge.
type digestChallenge struct {
	realm     string
	nonce     string
	opaque    string
	algorithm string // MD5, MD5-sess, SHA-256, SHA-256-sess (upper case)
	qop       []string
	stale     bool
	userhash  bool
	charset   string
}

// parseDigestChallenges reads every Digest challenge from the
// WWW-Authenticate values, in order.
func parseDigestChallenges(values []string) []digestChallenge {
	var out []digestChallenge
	for _, v := range values {
		for _, raw := range splitChallenges(v) {
			scheme, params, _ := strings.Cut(strings.TrimSpace(raw), " ")
			if !strings.EqualFold(scheme, "Digest") {
				continue
			}
			c := digestChallenge{algorithm: "MD5"}
			for k, val := range parseAuthParams(params) {
				switch strings.ToLower(k) {
				case "realm":
					c.realm = val
				case "nonce":
					c.nonce = val
				case "opaque":
					c.opaque = val
				case "algorithm":
					c.algorithm = strings.ToUpper(val)
				case "qop":
					for _, q := range strings.Split(val, ",") {
						if q = strings.TrimSpace(q); q != "" {
							c.qop = append(c.qop, strings.ToLower(q))
						}
					}
				case "stale":
					c.stale = strings.EqualFold(val, "true")
				case "userhash":
					c.userhash = strings.EqualFold(val, "true")
				case "charset":
					c.charset = val
				}
			}
			if c.nonce != "" {
				out = append(out, c)
			}
		}
	}
	return out
}

// splitChallenges separates the challenges in one header value, which
// RFC 7235 lets a server list with commas, without cutting inside the
// quoted, comma-bearing parameter values.
func splitChallenges(v string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '"' && (i == 0 || v[i-1] != '\\'):
			inQuote = !inQuote
			cur.WriteByte(c)
		case c == ',' && !inQuote:
			// A comma starts a new challenge only when a scheme name (a
			// word followed by a space, not '=') follows.
			rest := strings.TrimSpace(v[i+1:])
			if word, _, _ := strings.Cut(rest, " "); word != "" && !strings.Contains(word, "=") && strings.Contains(rest, " ") {
				out = append(out, cur.String())
				cur.Reset()
				continue
			}
			cur.WriteByte(c)
		default:
			cur.WriteByte(c)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// parseAuthParams reads `k=v, k="quoted, value"` pairs.
func parseAuthParams(s string) map[string]string {
	out := map[string]string{}
	for len(s) > 0 {
		s = strings.TrimLeft(s, " \t,")
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		key := strings.TrimSpace(s[:eq])
		s = strings.TrimLeft(s[eq+1:], " \t")
		var val string
		if strings.HasPrefix(s, `"`) {
			var b strings.Builder
			i := 1
			for ; i < len(s); i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
					b.WriteByte(s[i])
					continue
				}
				if s[i] == '"' {
					break
				}
				b.WriteByte(s[i])
			}
			val = b.String()
			if i < len(s) {
				i++
			}
			s = s[i:]
		} else {
			end := strings.IndexByte(s, ',')
			if end < 0 {
				end = len(s)
			}
			val = strings.TrimSpace(s[:end])
			s = s[end:]
		}
		out[key] = val
	}
	return out
}

// digestHash returns the hash the challenge names, or nil when apic does
// not implement it (RFC 7616 also lists SHA-512-256, which few servers
// offer; it is skipped in favour of the next challenge).
func digestHash(algorithm string) func() hash.Hash {
	switch strings.TrimSuffix(algorithm, "-SESS") {
	case "MD5":
		return md5.New
	case "SHA-256":
		return sha256.New
	}
	return nil
}

func hexHash(h func() hash.Hash, s string) string {
	hh := h()
	_, _ = io.WriteString(hh, s)
	return hex.EncodeToString(hh.Sum(nil))
}

// digestResponse computes the response field of RFC 7616 section 3.4.1 for
// one request, and returns the Authorization header value.
func digestResponse(c digestChallenge, user, pass, method, uri string, body []byte, cnonce string, nc int) (string, bool) {
	h := digestHash(c.algorithm)
	if h == nil {
		return "", false
	}
	// qop: prefer auth; auth-int only when the server offers nothing
	// else or the request carries a body to protect.
	qop := ""
	hasAuth, hasAuthInt := false, false
	for _, q := range c.qop {
		switch q {
		case "auth":
			hasAuth = true
		case "auth-int":
			hasAuthInt = true
		}
	}
	switch {
	case hasAuthInt && (len(body) > 0 || !hasAuth):
		qop = "auth-int"
	case hasAuth:
		qop = "auth"
	}
	ncText := fmt.Sprintf("%08x", nc)
	a1 := hexHash(h, user+":"+c.realm+":"+pass)
	if strings.HasSuffix(c.algorithm, "-SESS") {
		a1 = hexHash(h, a1+":"+c.nonce+":"+cnonce)
	}
	a2 := method + ":" + uri
	if qop == "auth-int" {
		a2 += ":" + hexHash(h, string(body))
	}
	ha2 := hexHash(h, a2)
	var response string
	if qop == "" {
		response = hexHash(h, a1+":"+c.nonce+":"+ha2)
	} else {
		response = hexHash(h, a1+":"+c.nonce+":"+ncText+":"+cnonce+":"+qop+":"+ha2)
	}
	username := user
	if c.userhash {
		username = hexHash(h, user+":"+c.realm)
	}
	var b strings.Builder
	fmt.Fprintf(&b, `Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
		quoteParam(username), quoteParam(c.realm), quoteParam(c.nonce), quoteParam(uri), response)
	if c.userhash {
		b.WriteString(", userhash=true")
	}
	fmt.Fprintf(&b, ", algorithm=%s", c.algorithm)
	if qop != "" {
		fmt.Fprintf(&b, `, qop=%s, nc=%s, cnonce="%s"`, qop, ncText, cnonce)
	}
	if c.opaque != "" {
		fmt.Fprintf(&b, `, opaque="%s"`, quoteParam(c.opaque))
	}
	return b.String(), true
}

func quoteParam(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// DigestState remembers the challenges seen in one invocation, per
// server, so a second request to the same server sends its Authorization
// on the first try with the nonce count incremented. A Runner holds one.
type DigestState struct {
	mu   sync.Mutex
	seen map[string]*digestEntry // by scheme://host
}

type digestEntry struct {
	challenge digestChallenge
	nc        int
}

// NewDigestState returns an empty state.
func NewDigestState() *DigestState { return &DigestState{seen: map[string]*digestEntry{}} }

func (s *DigestState) get(key string) (digestChallenge, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.seen[key]
	if !ok {
		return digestChallenge{}, 0, false
	}
	e.nc++
	return e.challenge, e.nc, true
}

func (s *DigestState) set(key string, c digestChallenge) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[key] = &digestEntry{challenge: c, nc: 1}
	return 1
}

// DigestTransport answers a server's Digest challenge on behalf of one
// request: it sends the request, and on a 401 that carries a Digest
// challenge it sends it once more with the Authorization header computed
// from the credentials. Rounds counts the requests it sent.
type DigestTransport struct {
	Base   http.RoundTripper
	User   string
	Pass   string
	State  *DigestState // may be nil: then every request is challenged
	Rounds int
	// Challenged is set when a 401 was answered; Answered when the
	// Authorization header went out (on the first or the second try).
	Challenged, Answered bool
	cnonce               func() string // tests fix it
}

// RoundTrip implements http.RoundTripper.
func (d *DigestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := d.Base
	if base == nil {
		base = http.DefaultTransport
	}
	key := req.URL.Scheme + "://" + req.URL.Host
	body, err := requestBody(req)
	if err != nil {
		return nil, err
	}
	first := req
	var pre *digestChallenge
	if d.State != nil {
		if c, nc, ok := d.State.get(key); ok {
			if r, ok := d.authorized(req, c, nc, body); ok {
				first, pre = r, &c
				d.Answered = true
			}
		}
	}
	d.Rounds++
	resp, err := base.RoundTrip(first)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	challenges := parseDigestChallenges(resp.Header.Values("WWW-Authenticate"))
	var chosen *digestChallenge
	for i := range challenges {
		if digestHash(challenges[i].algorithm) != nil {
			chosen = &challenges[i]
			break
		}
	}
	if chosen == nil {
		return resp, nil
	}
	// A stale nonce, or a server that wants a fresh challenge answered,
	// is the one case a second request is worth sending; a 401 to a
	// correct answer would be the wrong password, which the caller sees.
	if pre != nil && !chosen.stale && pre.nonce == chosen.nonce {
		return resp, nil
	}
	nc := 1
	if d.State != nil {
		nc = d.State.set(key, *chosen)
	}
	retry, ok := d.authorized(req, *chosen, nc, body)
	if !ok {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	d.Rounds++
	d.Challenged, d.Answered = true, true
	return base.RoundTrip(retry)
}

// authorized returns a copy of req with the Digest Authorization header for
// the challenge and a fresh body.
func (d *DigestTransport) authorized(req *http.Request, c digestChallenge, nc int, body []byte) (*http.Request, bool) {
	cn := d.cnonce
	if cn == nil {
		cn = randomCnonce
	}
	uri := req.URL.RequestURI()
	value, ok := digestResponse(c, d.User, d.Pass, req.Method, uri, body, cn(), nc)
	if !ok {
		return nil, false
	}
	out := req.Clone(req.Context())
	out.Header.Set("Authorization", value)
	if body != nil {
		out.Body = io.NopCloser(strings.NewReader(string(body)))
		out.ContentLength = int64(len(body))
	}
	return out, true
}

// requestBody reads a request's body so it can be hashed and replayed.
func requestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	if req.GetBody != nil {
		rc, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(rc)
	}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(strings.NewReader(string(data)))
	return data, nil
}

func randomCnonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
