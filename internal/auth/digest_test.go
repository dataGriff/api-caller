package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// TestDigestVectors pins the computation to the example of RFC 2617
// section 3.5 (the MD5 vector RFC 7616 keeps), then checks the SHA-256
// and -sess variants against the formulas of RFC 7616 section 3.4.
func TestDigestVectors(t *testing.T) {
	cs := parseDigestChallenges([]string{`Digest realm="testrealm@host.com", qop="auth,auth-int", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", opaque="5ccc069c403ebaf9f0171e9517f40e41"`})
	if len(cs) != 1 || cs[0].algorithm != "MD5" || len(cs[0].qop) != 2 {
		t.Fatalf("parsed %+v", cs)
	}
	got, ok := digestResponse(cs[0], "Mufasa", "Circle Of Life", "GET", "/dir/index.html", nil, "0a4f113b", 1)
	if !ok {
		t.Fatal("not answered")
	}
	p := parseAuthParams(strings.TrimPrefix(got, "Digest "))
	if p["response"] != "6629fae49393a05397450978507c4ef1" {
		t.Errorf("MD5 response %s, want 6629fae49393a05397450978507c4ef1\n%s", p["response"], got)
	}
	if p["qop"] != "auth" || p["nc"] != "00000001" || p["uri"] != "/dir/index.html" || p["opaque"] != "5ccc069c403ebaf9f0171e9517f40e41" || p["username"] != "Mufasa" || p["algorithm"] != "MD5" {
		t.Errorf("params %+v", p)
	}

	// The same inputs under SHA-256, SHA-256-sess and auth-int, computed
	// by hand from the RFC's formulas.
	h := digestHash("SHA-256")
	ha1 := hexHash(h, "Mufasa:testrealm@host.com:Circle Of Life")
	ha2 := hexHash(h, "GET:/dir/index.html")
	want := hexHash(h, ha1+":dcd98b7102dd2f0e8b11d0f600bfb0c093:00000002:0a4f113b:auth:"+ha2)
	c := cs[0]
	c.algorithm = "SHA-256"
	got, _ = digestResponse(c, "Mufasa", "Circle Of Life", "GET", "/dir/index.html", nil, "0a4f113b", 2)
	if p = parseAuthParams(strings.TrimPrefix(got, "Digest ")); p["response"] != want || p["nc"] != "00000002" || p["algorithm"] != "SHA-256" {
		t.Errorf("SHA-256: %+v want response %s", p, want)
	}
	c.algorithm = "SHA-256-SESS"
	sess := hexHash(h, ha1+":dcd98b7102dd2f0e8b11d0f600bfb0c093:0a4f113b")
	body := []byte(`{"a":1}`)
	ha2int := hexHash(h, "POST:/dir/index.html:"+hexHash(h, string(body)))
	want = hexHash(h, sess+":dcd98b7102dd2f0e8b11d0f600bfb0c093:00000001:0a4f113b:auth-int:"+ha2int)
	got, _ = digestResponse(c, "Mufasa", "Circle Of Life", "POST", "/dir/index.html", body, "0a4f113b", 1)
	if p = parseAuthParams(strings.TrimPrefix(got, "Digest ")); p["response"] != want || p["qop"] != "auth-int" || p["algorithm"] != "SHA-256-SESS" {
		t.Errorf("SHA-256-sess auth-int: %+v want response %s", p, want)
	}
	// Without qop the RFC 2069 form applies.
	c.algorithm = "MD5"
	c.qop = nil
	m := digestHash("MD5")
	want = hexHash(m, hexHash(m, "Mufasa:testrealm@host.com:Circle Of Life")+":dcd98b7102dd2f0e8b11d0f600bfb0c093:"+hexHash(m, "GET:/dir/index.html"))
	got, _ = digestResponse(c, "Mufasa", "Circle Of Life", "GET", "/dir/index.html", nil, "0a4f113b", 1)
	if p = parseAuthParams(strings.TrimPrefix(got, "Digest ")); p["response"] != want || p["qop"] != "" || p["nc"] != "" {
		t.Errorf("no qop: %+v want response %s", p, want)
	}
}

func TestDigestChallengeParsing(t *testing.T) {
	// Two challenges in one header, a quoted comma, a Basic one to skip, and
	// an unknown algorithm to pass over.
	values := []string{
		`Basic realm="site", Digest realm="a, b", nonce="n1", algorithm=SHA-512-256, qop="auth"`,
		`Digest realm="c", nonce="n2", stale=TRUE, userhash=true`,
	}
	cs := parseDigestChallenges(values)
	if len(cs) != 2 {
		t.Fatalf("challenges = %+v", cs)
	}
	if cs[0].realm != "a, b" || cs[0].nonce != "n1" || cs[0].algorithm != "SHA-512-256" {
		t.Errorf("first = %+v", cs[0])
	}
	if cs[1].realm != "c" || !cs[1].stale || !cs[1].userhash || cs[1].algorithm != "MD5" {
		t.Errorf("second = %+v", cs[1])
	}
	if digestHash(cs[0].algorithm) != nil {
		t.Error("SHA-512-256 should be unsupported")
	}
	if _, ok := digestResponse(cs[0], "u", "p", "GET", "/", nil, "c", 1); ok {
		t.Error("unsupported algorithm must not be answered")
	}
}

// digestServer challenges every request without a valid Authorization
// and verifies the ones that carry one by recomputing the response.
func digestServer(t *testing.T, algorithm, qop, nonce string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		challenge := `Digest realm="apic", nonce="` + nonce + `", algorithm=` + algorithm + `, qop="` + qop + `", opaque="op"`
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Digest ") {
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, "challenge", http.StatusUnauthorized)
			return
		}
		p := parseAuthParams(strings.TrimPrefix(authz, "Digest "))
		body, _ := io.ReadAll(r.Body)
		c := parseDigestChallenges([]string{challenge})[0]
		nc64, _ := strconv.ParseInt(p["nc"], 16, 32)
		nc := int(nc64)
		want, _ := digestResponse(c, "alice", "s3cret", r.Method, r.URL.RequestURI(), body, p["cnonce"], nc)
		wp := parseAuthParams(strings.TrimPrefix(want, "Digest "))
		if p["nonce"] != nonce {
			w.Header().Set("WWW-Authenticate", challenge+", stale=true")
			http.Error(w, "stale", http.StatusUnauthorized)
			return
		}
		if wp["response"] != p["response"] || p["opaque"] != "op" {
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "ok nc="+p["nc"]+" qop="+p["qop"])
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestDigestTransportAnswersChallengeAndReusesNonce(t *testing.T) {
	srv, hits := digestServer(t, "SHA-256", "auth", "nonce-1")
	state := NewDigestState()
	do := func(method, body string, pass string) (*http.Response, *DigestTransport) {
		t.Helper()
		dt := &DigestTransport{Base: srv.Client().Transport, User: "alice", Pass: pass, State: state}
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequestWithContext(context.Background(), method, srv.URL+"/thing?x=1", rd)
		resp, err := (&http.Client{Transport: dt}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp, dt
	}
	read := func(resp *http.Response) string {
		t.Helper()
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return string(data)
	}

	resp, dt := do("GET", "", "s3cret") //nolint:bodyclose // read closes it
	if resp.StatusCode != 200 || read(resp) != "ok nc=00000001 qop=auth" || !dt.Challenged || dt.Rounds != 2 {
		t.Fatalf("first: %d rounds=%d challenged=%v", resp.StatusCode, dt.Rounds, dt.Challenged)
	}
	// The second request to the same server answers on the first try with
	// the next nonce count, body included.
	resp, dt = do("POST", `{"a":1}`, "s3cret") //nolint:bodyclose // read closes it
	if resp.StatusCode != 200 || read(resp) != "ok nc=00000002 qop=auth" || dt.Challenged || !dt.Answered || dt.Rounds != 1 {
		t.Fatalf("second: %d rounds=%d challenged=%v answered=%v", resp.StatusCode, dt.Rounds, dt.Challenged, dt.Answered)
	}
	if hits.Load() != 3 {
		t.Fatalf("server hits = %d", hits.Load())
	}
	// A wrong password is answered once and then reported as the 401 it is.
	resp, dt = do("GET", "", "wrong")
	if resp.StatusCode != 401 || dt.Rounds != 1 {
		t.Fatalf("wrong password: %d rounds=%d", resp.StatusCode, dt.Rounds)
	}
	_ = resp.Body.Close()
}

func TestDigestTransportHandlesStaleNonceAndAuthInt(t *testing.T) {
	srv, _ := digestServer(t, "MD5", "auth-int", "nonce-A")
	state := NewDigestState()
	// Prime the state with an old nonce, as if the server had restarted.
	state.set(srv.URL, digestChallenge{realm: "apic", nonce: "old", algorithm: "MD5", qop: []string{"auth-int"}, opaque: "op"})
	dt := &DigestTransport{Base: srv.Client().Transport, User: "alice", Pass: "s3cret", State: state}
	req, _ := http.NewRequestWithContext(context.Background(), "PUT", srv.URL+"/thing", strings.NewReader("payload"))
	resp, err := (&http.Client{Transport: dt}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || string(data) != "ok nc=00000001 qop=auth-int" || dt.Rounds != 2 || !dt.Challenged {
		t.Fatalf("stale: %d %q rounds=%d", resp.StatusCode, data, dt.Rounds)
	}
}

func TestDigestNeverAnswersAnotherHost(t *testing.T) {
	// api redirects to elsewhere, which challenges: the credentials belong
	// to api and stay there.
	var elsewhereAuthz []string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhereAuthz = append(elsewhereAuthz, r.Header.Get("Authorization"))
		w.Header().Set("WWW-Authenticate", `Digest realm="other", nonce="n", algorithm=MD5, qop="auth"`)
		http.Error(w, "challenge", http.StatusUnauthorized)
	}))
	t.Cleanup(elsewhere.Close)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/x", http.StatusFound)
	}))
	t.Cleanup(api.Close)
	dt := &DigestTransport{Base: http.DefaultTransport, User: "alice", Pass: "s3cret", State: NewDigestState()}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", api.URL+"/thing", nil)
	resp, err := (&http.Client{Transport: dt}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 401 || len(elsewhereAuthz) != 1 || elsewhereAuthz[0] != "" || dt.Answered {
		t.Fatalf("status=%d authz=%q answered=%v", resp.StatusCode, elsewhereAuthz, dt.Answered)
	}
	if dt.Host != api.URL {
		t.Fatalf("pinned to %q, want %q", dt.Host, api.URL)
	}
}

func TestDigestWithoutChallengePassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "authz="+r.Header.Get("Authorization"))
	}))
	t.Cleanup(srv.Close)
	dt := &DigestTransport{Base: srv.Client().Transport, User: "u", Pass: "p", State: NewDigestState()}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
	resp, err := (&http.Client{Transport: dt}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(data) != "authz=" || dt.Rounds != 1 || dt.Answered {
		t.Fatalf("got %q rounds=%d", data, dt.Rounds)
	}
}

func TestAPIKeyForms(t *testing.T) {
	cases := []struct {
		spec, header, query, value string
	}{
		{"apikey {{key}}", "X-Api-Key", "", "k1"},
		{"apikey {{key}} header=X-Auth-Token", "X-Auth-Token", "", "k1"},
		{"apikey {{key}} query=api_key", "", "api_key", "k1"},
		{`apikey {{key}} header=Authorization prefix="Token "`, "Authorization", "", "Token k1"},
	}
	for _, c := range cases {
		s, err := Parse(c.spec)
		if err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		s, _ = s.Render(func(v string) (string, error) { return strings.ReplaceAll(v, "{{key}}", "k1"), nil })
		req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://a.b/x?q=1", nil)
		if err := Apply(context.Background(), s, req, nil, &Env{}); err != nil {
			t.Fatalf("%s: %v", c.spec, err)
		}
		if c.header != "" && req.Header.Get(c.header) != c.value {
			t.Errorf("%s: header %s = %q", c.spec, c.header, req.Header.Get(c.header))
		}
		if c.query != "" && req.URL.Query().Get(c.query) != c.value {
			t.Errorf("%s: query %s = %q (%s)", c.spec, c.query, req.URL.Query().Get(c.query), req.URL)
		}
		if h, q := s.APIKeyPlacement(); h != c.header || q != c.query {
			t.Errorf("%s: placement = %q %q", c.spec, h, q)
		}
	}
	// The query form appends to the query as written: nothing is reordered,
	// re-encoded or dropped, even a pair url.ParseQuery would refuse.
	s, _ := Parse("apikey k1 query=api_key")
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://a.b/x?b=2&a=%2Fpath;v=1", nil)
	if err := Apply(context.Background(), s, req, nil, &Env{}); err != nil || req.URL.RawQuery != "b=2&a=%2Fpath;v=1&api_key=k1" {
		t.Fatalf("query form: %v %s", err, req.URL.RawQuery)
	}
	req, _ = http.NewRequestWithContext(context.Background(), "GET", "https://a.b/x", nil)
	if err := Apply(context.Background(), s, req, nil, &Env{}); err != nil || req.URL.RawQuery != "api_key=k1" {
		t.Fatalf("query form on a bare URL: %v %s", err, req.URL.RawQuery)
	}
	// A key containing '=' stays a key; header and query exclude each other.
	s, err := Parse("apikey abc=def==")
	if err != nil || s.Args[0] != "abc=def==" {
		t.Fatalf("key with '=': %v %+v", err, s)
	}
	for _, bad := range []string{"apikey", "apikey k header=A query=b", "apikey k more", "digest user", "digest"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}
