// Package runner executes parsed requests: it resolves variables, sends the
// request, evaluates captures and assertions, and persists captured values
// to the session.
package runner

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dataGriff/api-caller/internal/assert"
	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/env"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/selector"
	"github.com/dataGriff/api-caller/internal/session"
	"github.com/dataGriff/api-caller/internal/template"
)

// Version is stamped by the CLI for the User-Agent header.
var Version = "dev"

// Options controls a Runner.
type Options struct {
	Env       string            // environment name from http-client.env.json
	Vars      map[string]string // --var overrides
	NoSession bool              // do not read or write .apic/session.json
	Timeout   time.Duration     // default per-request timeout
	Insecure  bool              // skip TLS verification
	KeepGoing bool              // in a flow, continue after a failure
	Redact    bool              // mask every request value and capture in output (for CI logs)
	// MaxBodyBytes caps the response body read into memory; zero means
	// apic.yaml's maxBodyBytes, then DefaultMaxBodyBytes.
	MaxBodyBytes int64
	Session      *session.Store // use this store instead of opening .apic/session.json (tests use session.NewMemory())
	// Cookies switches the cookie jar on (--cookies); apic.yaml's `cookies:
	// true` does the same. CookieJar is the jar to use instead of opening
	// .apic/cookies.json (tests and `apic test` scenarios pass their own).
	Cookies   bool
	CookieJar *session.Jar
	// Retry is the default retry policy ("<n> [interval]", see ParseRetry)
	// for requests without `# @retry`; empty means apic.yaml's retry, then
	// none. NoRetry switches every retry off.
	Retry   string
	NoRetry bool
	// CACert, Cert and Key are the --cacert, --cert and --key flags: a PEM
	// bundle to trust and a client certificate to present. They override
	// apic.yaml's tls section and the environment's SSLConfiguration.
	CACert string
	Cert   string
	Key    string
	// Proxy is the --proxy flag, an http, https or socks5 URL that beats
	// apic.yaml's proxy and the environment; NoProxy (--no-proxy) sends
	// every request directly, whatever is configured.
	Proxy   string
	NoProxy bool
}

// Progress reports one failed attempt of a request that is being retried,
// before the runner waits and sends it again.
type Progress struct {
	Req     *httpfile.Request
	Attempt int    // the attempt that just failed, from 1
	Max     int    // attempts the policy allows
	Failure string // why it failed: the first failed assertion, or the error
}

// Runner executes requests for one project.
type Runner struct {
	Project *project.Project
	Envs    *env.Environments
	Session *session.Store
	// Jar is the cookie jar, nil unless cookies are switched on. It is
	// in-memory under --no-session and persisted to .apic/cookies.json
	// otherwise.
	Jar    *session.Jar
	Opts   Options
	Stderr io.Writer // interactive prompts such as device-code sign-in; nil means os.Stderr
	// Progress, when set, is called after each failed attempt of a request
	// that will be retried.
	Progress func(Progress)
	// OnResult, when set, is called by RunAll as each request finishes,
	// with its result (never nil) and the error, if any, that stopped it.
	OnResult func(*Result, error)

	sleep    func(ctx context.Context, d time.Duration) error // between attempts; tests replace it
	proxy    *proxySettings                                   // --proxy or apic.yaml's proxy; nil means the environment
	digest   *auth.DigestState                                // Digest challenges seen this invocation, per server
	results  map[string]*Result
	captured map[string]string
	tlsMu    sync.Mutex
	tlsCache map[string]*tls.Config
	// transports are shared across the requests of one invocation, keyed
	// by their TLS settings, so a flow reuses its connections.
	transports map[string]*http.Transport
}

// New builds a Runner, loading env files and the session.
func New(p *project.Project, opts Options) (*Runner, error) {
	for _, d := range p.Diagnostics {
		if d.Severity == "error" {
			return nil, usagef("%s:%d: %s (run `apic validate`)", d.Path, d.Line, d.Message)
		}
	}
	if opts.Env == "" {
		opts.Env = p.Config.Env
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
		if p.Config.Timeout != "" {
			d, err := time.ParseDuration(p.Config.Timeout)
			if err != nil {
				return nil, usagef("apic.yaml: bad timeout %q", p.Config.Timeout)
			}
			opts.Timeout = d
		}
	}
	envs, err := env.Load(p.Root)
	if err != nil {
		return nil, usagef("%v", err)
	}
	if opts.Env != "" && !envs.Has(opts.Env) {
		if names := envs.Names(); len(names) > 0 {
			return nil, usagef("environment %q not found in %s (have: %s)", opts.Env, env.PublicFile, strings.Join(names, ", "))
		}
		return nil, usagef("environment %q requested but no %s found in %s", opts.Env, env.PublicFile, p.Root)
	}
	// Flag paths are the user's own, relative to where they typed them,
	// which absolute paths tell apart from the project's confined ones.
	for _, p := range []*string{&opts.CACert, &opts.Cert, &opts.Key} {
		if *p != "" {
			if abs, err := filepath.Abs(*p); err == nil {
				*p = abs
			}
		}
	}
	r := &Runner{Project: p, Envs: envs, Opts: opts, Stderr: os.Stderr, results: map[string]*Result{}, captured: map[string]string{}, sleep: sleepCtx, digest: auth.NewDigestState()}
	if raw, fromFlag := opts.Proxy, opts.Proxy != ""; raw != "" || p.Config.Proxy != "" {
		if raw == "" {
			raw = p.Config.Proxy
		}
		u, err := parseProxy(raw, proxySource(fromFlag))
		if err != nil {
			return nil, err
		}
		r.proxy = &proxySettings{url: u, source: proxySource(fromFlag), noProxy: p.Config.NoProxy}
	}
	switch {
	case opts.Session != nil:
		r.Session = opts.Session
	case !opts.NoSession:
		if r.Session, err = session.Open(p.Root); err != nil {
			return nil, usagef("session: %v", err)
		}
	}
	if opts.Cookies || p.Config.Cookies {
		switch {
		case opts.CookieJar != nil:
			r.Jar = opts.CookieJar
		case opts.NoSession:
			r.Jar = session.NewMemoryJar()
		default:
			if r.Jar, err = session.OpenJar(p.Root); err != nil {
				return nil, usagef("cookies: %v", err)
			}
		}
	}
	return r, nil
}

// Resolved is a request with every placeholder substituted.
type Resolved struct {
	Name    string            `json:"name,omitempty"`
	File    string            `json:"file"`
	Line    int               `json:"line"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers []httpfile.Header `json:"-"`
	Body    string            `json:"-"`
	Auth    string            `json:"auth,omitempty"` // auth type applied, e.g. "aws"
	// TLS is the non-default TLS setup for this request's host: a private
	// CA, a client certificate or no verification.
	TLS *TLSInfo `json:"tls,omitempty"`
	// Proxy is the proxy the request goes through, when one is configured
	// by flag, apic.yaml or the environment.
	Proxy    *ProxyInfo `json:"proxy,omitempty"`
	AuthSpec *auth.Spec `json:"-"` // rendered spec (contains secrets)
	// SecretHeaders names headers whose value came from a secret source
	// (private env file, .env, session or a capture).
	SecretHeaders map[string]bool `json:"-"`
	// Parts are the parts of a multipart/form-data body, for renderers and
	// the curl exporter; Body then holds a one-line summary of them.
	Parts   []FormPart `json:"-"`
	rawBody []byte     // the assembled multipart body, sent as is
	missing []string
}

// FormPart is one part of a resolved multipart/form-data body. Value is
// the rendered text of a text part; File is the path of a file part,
// relative to the project root, as `apic curl` needs it.
type FormPart struct {
	Name        string
	Filename    string
	ContentType string
	Value       string
	File        string
	Size        int
}

// BodyBytes returns the bytes the request sends: the assembled multipart
// body when there is one, else the rendered body.
func (r Resolved) BodyBytes() []byte {
	if r.rawBody != nil {
		return r.rawBody
	}
	return []byte(r.Body)
}

// MarshalJSON renders headers as an object (sensitive values masked) and the
// body as a string. Result.MarshalJSON applies --redact on top.
func (r Resolved) MarshalJSON() ([]byte, error) {
	return r.marshal(false)
}

func (r Resolved) marshal(redact bool) ([]byte, error) {
	type alias Resolved
	return json.Marshal(struct {
		alias
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body,omitempty"`
	}{alias(r.view(redact)), headerMap(r.DisplayHeaders(redact)), r.DisplayBody(redact)})
}

// Masked is what a hidden value is replaced with in output.
const Masked = "***"

// sensitiveHeaders are masked in every output regardless of where their
// value came from.
var sensitiveHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "cookie": true,
	"x-api-key": true, "x-auth-token": true, "api-key": true, "x-amz-security-token": true,
}

// DisplayHeaders returns the request headers with sensitive values masked;
// with redact set every value is masked.
func (r Resolved) DisplayHeaders(redact bool) []httpfile.Header {
	out := make([]httpfile.Header, len(r.Headers))
	for i, h := range r.Headers {
		out[i] = h
		if redact || sensitiveHeaders[strings.ToLower(h.Name)] || r.SecretHeaders[h.Name] {
			out[i].Value = Masked
		}
	}
	return out
}

// DisplayBody returns the body, or the mask when redacting.
func (r Resolved) DisplayBody(redact bool) string {
	if redact && r.Body != "" {
		return Masked
	}
	return r.Body
}

// DisplayURL returns the URL with query values masked when redacting.
func (r Resolved) DisplayURL(redact bool) string {
	if !redact {
		return r.URL
	}
	base, query, ok := strings.Cut(r.URL, "?")
	if !ok {
		return r.URL
	}
	parts := strings.Split(query, "&")
	for i, p := range parts {
		if k, _, has := strings.Cut(p, "="); has {
			parts[i] = k + "=" + Masked
		}
	}
	return base + "?" + strings.Join(parts, "&")
}

func (r Resolved) view(redact bool) Resolved {
	out := r
	out.URL = r.DisplayURL(redact)
	return out
}

func headerMap(hs []httpfile.Header) map[string]string {
	m := map[string]string{}
	for _, h := range hs {
		m[h.Name] = h.Value
	}
	return m
}

// Response is the JSON-friendly view of a response.
type Response struct {
	Status     int               `json:"status"`
	StatusText string            `json:"status_text"`
	Headers    map[string]string `json:"headers"`
	Body       any               `json:"body"` // parsed JSON when the body is JSON, else a string
	DurationMs int64             `json:"duration_ms"`
	Size       int               `json:"size"`
	// Timings is where the round trip went, from net/http/httptrace.
	Timings *Timings `json:"timings,omitempty"`
}

// Timings breaks a round trip down: name resolution, the TCP connection,
// the TLS handshake, the wait for the first response byte, and the total
// including the body. A reused connection has no DNS, connect or TLS
// time. Redirects and a digest challenge add their hops' times together.
type Timings struct {
	DNSMs     int64 `json:"dns_ms"`
	ConnectMs int64 `json:"connect_ms"`
	TLSMs     int64 `json:"tls_ms"`
	TTFBMs    int64 `json:"ttfb_ms"`
	TotalMs   int64 `json:"total_ms"`
	Reused    bool  `json:"reused"`
}

// traceTimes collects httptrace callbacks for one request.
type traceTimes struct {
	start                      time.Time
	dnsStart, connStart, tlsAt time.Time
	dns, connect, tls          time.Duration
	firstByte                  time.Time
	reused, gotConn            bool
}

func (t *traceTimes) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { t.dnsStart = time.Now() },
		DNSDone: func(httptrace.DNSDoneInfo) {
			if !t.dnsStart.IsZero() {
				t.dns += time.Since(t.dnsStart)
			}
		},
		ConnectStart: func(string, string) { t.connStart = time.Now() },
		ConnectDone: func(string, string, error) {
			if !t.connStart.IsZero() {
				t.connect += time.Since(t.connStart)
			}
		},
		TLSHandshakeStart: func() { t.tlsAt = time.Now() },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			if !t.tlsAt.IsZero() {
				t.tls += time.Since(t.tlsAt)
			}
		},
		GotConn:              func(info httptrace.GotConnInfo) { t.reused, t.gotConn = info.Reused, true },
		GotFirstResponseByte: func() { t.firstByte = time.Now() },
	}
}

func (t *traceTimes) timings(total time.Duration) *Timings {
	out := &Timings{DNSMs: t.dns.Milliseconds(), ConnectMs: t.connect.Milliseconds(), TLSMs: t.tls.Milliseconds(), TotalMs: total.Milliseconds(), Reused: t.reused}
	if !t.firstByte.IsZero() {
		out.TTFBMs = t.firstByte.Sub(t.start).Milliseconds()
	} else {
		out.TTFBMs = out.TotalMs
	}
	return out
}

// Result is the outcome of running one request.
type Result struct {
	OK       bool              `json:"ok"`
	Request  Resolved          `json:"request"`
	Response *Response         `json:"response,omitempty"`
	Captures map[string]string `json:"captures,omitempty"`
	Asserts  []assert.Result   `json:"asserts,omitempty"`
	Errors   []string          `json:"errors,omitempty"`
	// Attempts is how many times the request was sent under a `# @retry`
	// policy (or --retry, or retry in apic.yaml); zero when none applied.
	Attempts int `json:"attempts,omitempty"`
	// Deps are the results of the requests `# @ref` and `# @forceRef` ran
	// first, in run order; a dependency's own dependencies nest under it.
	Deps   []*Result `json:"ran_first,omitempty"`
	Redact bool      `json:"-"` // set from Options.Redact
	raw    *selector.Response
	req    *httpfile.Request
	// authNote says what the auth did on the wire beyond setting a header:
	// for digest, whether a challenge was answered. Shown by run -v.
	authNote string
}

// AuthNote reports what the request's auth did on the wire, for verbose
// output: "digest: 401 challenge answered (2 requests)" and the like.
// Empty for the header-setting types.
func (r *Result) AuthNote() string { return r.authNote }

// Req returns the request this result is for, so a caller holding results
// of dependencies (Deps) can map them back to the requests that produced
// them. It is nil for results built by hand.
func (r *Result) Req() *httpfile.Request { return r.req }

// MarshalJSON masks sensitive request and response headers always, and
// everything (headers, bodies, query values, captures and assertion values)
// when Redact is set.
//
// The shadowing fields sit at a shallower depth than the embedded alias, so
// encoding/json picks them over the originals.
func (r Result) MarshalJSON() ([]byte, error) {
	type alias Result
	out := struct {
		alias
		Request  json.RawMessage   `json:"request"`
		Captures map[string]string `json:"captures,omitempty"`
		Response *Response         `json:"response,omitempty"`
		Asserts  []assert.Result   `json:"asserts,omitempty"`
	}{alias: alias(r), Captures: r.DisplayCaptures(), Response: r.DisplayResponse(), Asserts: r.DisplayAsserts()}
	req, err := r.Request.marshal(r.Redact)
	if err != nil {
		return nil, err
	}
	out.Request = req
	return json.Marshal(out)
}

// DisplayCaptures returns captures, masked when redacting.
func (r Result) DisplayCaptures() map[string]string {
	if !r.Redact || len(r.Captures) == 0 {
		return r.Captures
	}
	out := map[string]string{}
	for k := range r.Captures {
		out[k] = Masked
	}
	return out
}

// Raw returns the underlying response for renderers.
func (r *Result) Raw() *selector.Response { return r.raw }

// Resolve substitutes variables in a request without sending it.
func (r *Runner) Resolve(req *httpfile.Request) (*Resolved, error) {
	res := &Resolved{Name: req.Name, File: req.File.Path, Line: req.Line, Method: req.Method}
	var missing []string
	render := func(s string) (string, error) {
		out, err := template.Render(s, func(e string) (string, bool, error) { return r.resolveExpr(req, e) })
		var me *template.MissingError
		if errors.As(err, &me) {
			missing = append(missing, me.Exprs...)
			return out, nil
		}
		return out, err
	}
	var err error
	if res.URL, err = render(strings.TrimSpace(req.URL)); err != nil {
		return nil, usagef("%s:%d: %v", req.File.Path, req.Line, err)
	}
	res.TLS = r.tlsInfoForURL(res.URL)
	res.Proxy = r.ProxyInfo(res.URL)
	res.SecretHeaders = map[string]bool{}
	for _, h := range req.Headers {
		v, err := render(h.Value)
		if err != nil {
			return nil, usagef("%s:%d: header %s: %v", req.File.Path, req.Line, h.Name, err)
		}
		res.Headers = append(res.Headers, httpfile.Header{Name: h.Name, Value: v})
		for _, e := range template.Exprs(h.Value) {
			if _, _, secret, _ := r.resolveExprMeta(req, e, 0); secret {
				res.SecretHeaders[h.Name] = true
			}
		}
	}
	body := req.Body
	if req.BodyFile != "" {
		data, err := r.readBodyFile(req, req.BodyFile, "body file")
		if err != nil {
			return nil, err
		}
		body = string(data)
		if !req.BodyFileTemplated {
			res.Body = body
			body = ""
		}
	}
	if m, err := req.Multipart(); err != nil {
		return nil, usagef("%s:%d: %v", req.File.Path, req.Line, err)
	} else if m != nil {
		if res.rawBody, res.Parts, err = r.multipartBody(req, m, render); err != nil {
			return nil, err
		}
		res.Body = fmt.Sprintf("<multipart: %d part%s, %d file%s>", len(m.Parts), plural(len(m.Parts)), m.Files(), plural(m.Files()))
		body = ""
	}
	if body != "" {
		if res.Body, err = render(body); err != nil {
			return nil, usagef("%s:%d: body: %v", req.File.Path, req.Line, err)
		}
	}
	if spec, err := r.authSpec(req); err != nil {
		return nil, err
	} else if spec != nil {
		rendered, err := spec.Render(render)
		if err != nil {
			return nil, usagef("%s:%d: @auth: %v", req.File.Path, req.Line, err)
		}
		res.Auth, res.AuthSpec = spec.Type, rendered
	}
	res.missing = dedupe(missing)
	return res, nil
}

// authSpec returns the parsed auth spec for a request: its own `# @auth`
// directive, else auth.default from apic.yaml, else nil.
func (r *Runner) authSpec(req *httpfile.Request) (*auth.Spec, error) {
	raw, ok := req.Directive("auth")
	where := fmt.Sprintf("%s:%d", req.File.Path, req.Line)
	if !ok {
		raw = r.Project.Config.Auth.Default
		where = project.ConfigFile + " auth.default"
		if strings.TrimSpace(raw) == "" {
			return nil, nil
		}
	}
	spec, err := auth.Parse(raw)
	if err != nil {
		return nil, usagef("%s: %v", where, err)
	}
	if spec.Type == "none" {
		return nil, nil
	}
	return spec, nil
}

// AuthSource reports the auth spec template for describe: the raw text and
// where it was declared.
func (r *Runner) AuthSource(req *httpfile.Request) (raw, source string) {
	if v, ok := req.Directive("auth"); ok {
		return v, "request"
	}
	if r.Project.Config.Auth.Default != "" {
		return r.Project.Config.Auth.Default, project.ConfigFile
	}
	return "", ""
}

// sessionCache adapts the session store to auth.Cache, scoped to the
// current environment.
type sessionCache struct {
	r *Runner
}

func (c sessionCache) Get(key string) (string, bool) {
	if c.r.Session == nil {
		return "", false
	}
	return c.r.Session.Get(c.r.Opts.Env, key)
}

func (c sessionCache) Set(key, value string) error {
	if c.r.Session == nil {
		return nil
	}
	c.r.Session.Set(c.r.Opts.Env, map[string]string{key: value})
	return c.r.Session.Save()
}

func (r *Runner) authEnv() (*auth.Env, error) {
	// Token endpoints get the project-wide settings, not a host override.
	tr, err := r.transport("")
	if err != nil {
		return nil, err
	}
	e := &auth.Env{
		AllowExec: r.Project.Config.Auth.AllowExec,
		Stderr:    r.Stderr,
		Client:    &http.Client{Timeout: r.Opts.Timeout, Transport: tr},
	}
	if r.Session != nil {
		e.Cache = sessionCache{r}
	}
	return e, nil
}

// Render substitutes {{placeholders}} in arbitrary text using the runner's
// variables (no file-level @vars, since no request is in scope).
func (r *Runner) Render(s string) (string, error) {
	return template.Render(s, func(e string) (string, bool, error) { return r.resolveExpr(nil, e) })
}

// Capture stores a value in the capture layer, below --var and shell
// overrides and above the environment files, exactly like `# @capture`.
func (r *Runner) Capture(name, value string) {
	if r.captured == nil {
		r.captured = map[string]string{}
	}
	r.captured[name] = value
}

// Results returns the named responses of this run, for
// `{{name.response...}}` references.
func (r *Runner) Results() map[string]*Result {
	out := make(map[string]*Result, len(r.results))
	for k, v := range r.results {
		out[k] = v
	}
	return out
}

// SetResult registers a named response, e.g. one carried over from
// another runner.
func (r *Runner) SetResult(name string, res *Result) {
	if r.results == nil {
		r.results = map[string]*Result{}
	}
	r.results[name] = res
}

// Captured returns a copy of the values captured during this run.
func (r *Runner) Captured() map[string]string {
	out := make(map[string]string, len(r.captured))
	for k, v := range r.captured {
		out[k] = v
	}
	return out
}

// SetVar adds or overrides a variable at --var precedence.
func (r *Runner) SetVar(name, value string) {
	if r.Opts.Vars == nil {
		r.Opts.Vars = map[string]string{}
	}
	r.Opts.Vars[name] = value
}

// MissingError explains unresolved variables with a hint on how to provide them.
func (r *Runner) MissingError(req *httpfile.Request, missing []string) error {
	var parts []string
	for _, m := range missing {
		info, _, _ := r.lookup(req, m, 0)
		hint := fmt.Sprintf("pass --var %s=... or add it to %s", m, env.PublicFile)
		if info.CapturedBy != "" {
			hint = fmt.Sprintf("it is captured by request %q; run `apic run %s` first, or pass --var %s=...", info.CapturedBy, info.CapturedBy, m)
		} else if strings.HasPrefix(m, "$") || strings.Contains(m, ".response.") {
			hint = "built-in or response reference could not be resolved"
		}
		parts = append(parts, fmt.Sprintf("{{%s}}: %s", m, hint))
	}
	return usagef("%s:%d: missing variable%s\n  %s", req.File.Path, req.Line, plural(len(missing)), strings.Join(parts, "\n  "))
}

func refKey(ref httpfile.Ref) string {
	if ref.Force {
		return "@forceRef"
	}
	return "@ref"
}

// cycleError names a `# @ref` chain that leads back to a request already
// on it, as "a -> b -> a".
func (r *Runner) cycleError(req *httpfile.Request, ref httpfile.Ref, chain []*httpfile.Request, target *httpfile.Request) error {
	var ids []string
	for _, c := range chain {
		if c == target || len(ids) > 0 {
			ids = append(ids, c.ID())
		}
	}
	ids = append(ids, target.ID())
	return usagef("%s:%d: %s %s is a cycle: %s", req.File.Path, ref.Line, refKey(ref), ref.ID, strings.Join(ids, " -> "))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Run resolves, sends and evaluates a single request. A non-nil error is a
// usage or transport problem; assertion failures are reported in Result.OK.
//
// Requests the request declares with `# @forceRef` run first every time;
// those declared with `# @ref` run first only when a variable is missing,
// and once per Runner. Their results ride in Result.Deps. A dependency
// that fails stops the request: the result is not OK and names it.
func (r *Runner) Run(ctx context.Context, req *httpfile.Request) (*Result, error) {
	return r.run(ctx, req, nil, map[*httpfile.Request]bool{})
}

// refTarget resolves the target of a `# @ref` to exactly one request.
func (r *Runner) refTarget(req *httpfile.Request, ref httpfile.Ref) (*httpfile.Request, error) {
	key := "@ref"
	if ref.Force {
		key = "@forceRef"
	}
	targets, err := r.Project.Resolve(ref.ID)
	if err != nil {
		return nil, usagef("%s:%d: %s %s: %v", req.File.Path, ref.Line, key, ref.ID, err)
	}
	if len(targets) != 1 {
		return nil, usagef("%s:%d: %s %s names %d requests; refer to one request by name or file#name", req.File.Path, ref.Line, key, ref.ID, len(targets))
	}
	return targets[0], nil
}

// run is Run with the chain of requests whose refs led here, for cycle
// detection and the error that names the cycle, and the `# @ref` targets
// this invocation has already run, so a dependency reached twice runs
// once. Both are per invocation: the next Run starts afresh.
func (r *Runner) run(ctx context.Context, req *httpfile.Request, chain []*httpfile.Request, ran map[*httpfile.Request]bool) (*Result, error) {
	var deps []*Result
	// failed builds the result of a request that never went out because a
	// dependency failed, keeping what the dependency produced.
	failed := func(msg string) *Result {
		return &Result{Request: Resolved{Name: req.Name, File: req.File.Path, Line: req.Line, Method: req.Method, URL: req.URL},
			Errors: []string{msg}, Deps: deps, Redact: r.Opts.Redact, req: req}
	}
	runDep := func(ref httpfile.Ref) (*Result, error) {
		target, err := r.refTarget(req, ref)
		if err != nil {
			return failed(err.Error()), err
		}
		next := make([]*httpfile.Request, len(chain)+1)
		copy(next, chain)
		next[len(chain)] = req
		for _, c := range next {
			if c == target {
				return nil, r.cycleError(req, ref, next, target)
			}
		}
		ran[target] = true
		dep, err := r.run(ctx, target, next, ran)
		if dep != nil {
			deps = append(deps, dep)
		}
		if err != nil {
			return failed(fmt.Sprintf("%s %s: %v", refKey(ref), ref.ID, err)), err
		}
		if !dep.OK {
			return failed(fmt.Sprintf("%s %s failed", refKey(ref), ref.ID)), nil
		}
		return nil, nil
	}
	refs := req.Refs()
	for _, ref := range refs {
		if !ref.Force {
			continue
		}
		if res, err := runDep(ref); res != nil || err != nil {
			return res, err
		}
	}
	resolved, err := r.Resolve(req)
	if err != nil {
		return nil, err
	}
	if len(resolved.missing) > 0 {
		pulled := false
		for _, ref := range refs {
			if ref.Force {
				continue
			}
			if target, err := r.refTarget(req, ref); err == nil && ran[target] {
				continue
			}
			if res, err := runDep(ref); res != nil || err != nil {
				return res, err
			}
			pulled = true
		}
		if pulled {
			if resolved, err = r.Resolve(req); err != nil {
				return nil, err
			}
		}
	}
	if len(resolved.missing) > 0 {
		return nil, r.MissingError(req, resolved.missing)
	}
	result := &Result{Request: *resolved, OK: true, Redact: r.Opts.Redact, Deps: deps, req: req}
	preparedAsserts := make([]preparedAssert, 0, len(req.Asserts))
	for _, a := range req.Asserts {
		expr, err := assert.Parse(a.Expr)
		if err != nil {
			return nil, usagef("%s:%d: %v", req.File.Path, a.Line, err)
		}
		expected := expr.Value
		if expr.Op != "exists" && expr.Op != "not exists" {
			expected, err = template.Render(expr.Value, func(e string) (string, bool, error) { return r.resolveExpr(req, e) })
			if err != nil {
				var me *template.MissingError
				if errors.As(err, &me) {
					return nil, r.MissingError(req, dedupe(me.Exprs))
				}
				return nil, usagef("%s:%d: assert %q: %v", req.File.Path, a.Line, a.Expr, err)
			}
		}
		preparedAsserts = append(preparedAsserts, preparedAssert{expr: expr, expected: expected, raw: a.Expr})
	}

	timeout := r.Opts.Timeout
	if t, ok := req.Directive("timeout"); ok {
		d, err := time.ParseDuration(t)
		if err != nil {
			return nil, usagef("%s:%d: bad @timeout %q", req.File.Path, req.Line, t)
		}
		timeout = d
	}
	policy, err := r.retryPolicy(req)
	if err != nil {
		return nil, err
	}

	for attempt := 1; ; attempt++ {
		res, err := r.attempt(ctx, req, resolved, preparedAsserts, timeout)
		if err != nil {
			var ue *UsageError
			if errors.As(err, &ue) || attempt >= policy.n {
				return nil, err
			}
			r.report(Progress{Req: req, Attempt: attempt, Max: policy.n, Failure: err.Error()})
		} else {
			if policy.n > 1 {
				res.Attempts = attempt
			}
			if res.OK || attempt >= policy.n {
				result.Response, result.raw = res.Response, res.raw
				result.Captures, result.Asserts, result.Attempts = res.Captures, res.Asserts, res.Attempts
				result.authNote = res.authNote
				result.Errors = append(result.Errors, res.Errors...)
				result.OK = result.OK && res.OK
				break
			}
			r.report(Progress{Req: req, Attempt: attempt, Max: policy.n, Failure: res.Problem()})
		}
		if err := r.sleep(ctx, policy.interval); err != nil {
			return nil, &TransportError{Err: err}
		}
	}

	// Only the attempt that counts commits its captures, for this run and
	// for later ones.
	for k, v := range result.Captures {
		r.captured[k] = v
	}
	if req.Name != "" {
		r.results[req.Name] = result
	}
	if len(result.Captures) > 0 && r.Session != nil {
		if _, off := req.Directive("no-session"); !off {
			r.Session.Set(r.Opts.Env, result.Captures)
			if err := r.Session.Save(); err != nil {
				result.Errors = append(result.Errors, "session: "+err.Error())
				result.OK = false
			}
		}
	}
	if r.Jar != nil {
		if err := r.Jar.Save(); err != nil {
			result.Errors = append(result.Errors, "cookies: "+err.Error())
			result.OK = false
		}
	}
	return result, nil
}

// attempt sends a resolved request once and evaluates its captures and
// assertions into a fresh Result, without committing anything to the
// runner or the session. It has its own timeout, so a retry loop gives
// every attempt the full time.
func (r *Runner) attempt(ctx context.Context, req *httpfile.Request, resolved *Resolved, asserts []preparedAssert, timeout time.Duration) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var trace traceTimes
	ctx = httptrace.WithClientTrace(ctx, trace.trace())

	var body io.Reader
	if b := resolved.BodyBytes(); len(b) > 0 {
		body = bytes.NewReader(b)
	}
	httpReq, err := http.NewRequestWithContext(ctx, resolved.Method, resolved.URL, body)
	if err != nil {
		return nil, usagef("%s:%d: %v", req.File.Path, req.Line, err)
	}
	httpReq.Header.Set("User-Agent", "apic/"+Version)
	for _, h := range resolved.Headers {
		if strings.EqualFold(h.Name, "Host") {
			httpReq.Host = h.Value
			continue
		}
		httpReq.Header.Add(h.Name, h.Value)
	}

	if resolved.AuthSpec != nil {
		env, err := r.authEnv()
		if err != nil {
			return nil, err
		}
		if err := auth.Apply(ctx, resolved.AuthSpec, httpReq, resolved.BodyBytes(), env); err != nil {
			return nil, usagef("%s:%d: auth: %v", req.File.Path, req.Line, err)
		}
	}

	client, err := r.client(req, httpReq.URL.Host)
	if err != nil {
		return nil, err
	}
	var digest *auth.DigestTransport
	if s := resolved.AuthSpec; s != nil && s.Type == "digest" {
		digest = &auth.DigestTransport{Base: client.Transport, User: s.Args[0], Pass: s.Args[1], State: r.digest}
		client.Transport = digest
	}
	start := time.Now()
	trace.start = start
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, r.transportError(err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	// Bounded: an unbounded ReadAll lets one hostile or oversized response take
	// the process down, and the body is held more than once while it is parsed
	// and rendered.
	data, err := readBody(httpResp.Body, r.maxBodyBytes())
	if err != nil {
		return nil, r.transportError(err)
	}
	dur := time.Since(start)

	raw := &selector.Response{Status: httpResp.StatusCode, StatusText: statusText(httpResp.Status), Headers: httpResp.Header, Body: data, Duration: dur}
	result := &Result{OK: true, Redact: r.Opts.Redact, raw: raw, req: req}
	if digest != nil {
		switch {
		case digest.Challenged:
			result.authNote = fmt.Sprintf("digest: 401 challenge answered (%d requests)", digest.Rounds)
		case digest.Answered:
			result.authNote = "digest: answered from an earlier challenge"
		default:
			result.authNote = "digest: the server sent no challenge"
		}
	}
	result.Response = &Response{Status: raw.Status, StatusText: raw.StatusText, Headers: flatHeaders(httpResp.Header), Body: jsonOrString(data), DurationMs: dur.Milliseconds(), Size: len(data), Timings: trace.timings(dur)}

	for _, c := range req.Captures {
		v, ok, err := selector.Select(raw, c.Selector)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("capture %s: %v", c.Name, err))
			result.OK = false
			continue
		}
		if !ok {
			result.Errors = append(result.Errors, fmt.Sprintf("capture %s: nothing at %s", c.Name, c.Selector))
			result.OK = false
			continue
		}
		if result.Captures == nil {
			result.Captures = map[string]string{}
		}
		result.Captures[c.Name] = v
	}
	for _, a := range asserts {
		ar := assert.Eval(a.expr, a.expected, raw)
		if !ar.Pass {
			result.OK = false
		}
		result.Asserts = append(result.Asserts, ar)
	}
	return result, nil
}

type preparedAssert struct {
	expr     assert.Expr
	expected string
	raw      string
}

// Problem is the one-line reason a result is not OK: the first failed
// assertion with what it got, else the first error, else "".
func (r *Result) Problem() string {
	for _, a := range r.DisplayAsserts() {
		if a.Error != "" {
			return a.Expr + ": " + a.Error
		}
		if !a.Pass {
			return fmt.Sprintf("%s: got %q", a.Expr, truncate(a.Actual, 40))
		}
	}
	if len(r.Errors) > 0 {
		return r.Errors[0]
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// transportError wraps a network failure. Go's URL errors quote the full
// URL, query values included, so under Redact the URL is masked the way
// the rest of the output masks it.
func (r *Runner) transportError(err error) error {
	var ue *url.Error
	if r.Opts.Redact && errors.As(err, &ue) {
		return &TransportError{Err: fmt.Errorf("%s %s: %w", ue.Op, Masked, ue.Err)}
	}
	return &TransportError{Err: err}
}

func (r *Runner) report(p Progress) {
	if r.Progress != nil {
		r.Progress(p)
	}
}

// sleepCtx waits for d unless ctx ends first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retryPolicy is how many times a request may be sent and the wait
// between attempts.
type retryPolicy struct {
	n        int
	interval time.Duration
}

// retryPolicy picks the policy for a request: `# @retry` on the request,
// else Options.Retry (--retry), else retry in apic.yaml, else one attempt;
// Options.NoRetry makes it one attempt regardless.
func (r *Runner) retryPolicy(req *httpfile.Request) (retryPolicy, error) {
	one := retryPolicy{n: 1}
	if r.Opts.NoRetry {
		return one, nil
	}
	if v, ok := req.Directive("retry"); ok {
		n, d, err := httpfile.ParseRetry(v)
		if err != nil {
			return one, usagef("%s:%d: bad @retry %q: %v", req.File.Path, req.Line, v, err)
		}
		return retryPolicy{n, d}, nil
	}
	if r.Opts.Retry != "" {
		n, d, err := httpfile.ParseRetry(r.Opts.Retry)
		if err != nil {
			return one, usagef("--retry %q: %v", r.Opts.Retry, err)
		}
		return retryPolicy{n, d}, nil
	}
	if v := r.Project.Config.Retry; v != "" {
		n, d, err := httpfile.ParseRetry(v)
		if err != nil {
			return one, usagef("apic.yaml: bad retry %q: %v", v, err)
		}
		return retryPolicy{n, d}, nil
	}
	return one, nil
}

// RunAll runs requests in order as a flow, stopping at the first failure
// unless KeepGoing is set. Results for requests that ran are always returned.
func (r *Runner) RunAll(ctx context.Context, reqs []*httpfile.Request) ([]*Result, error) {
	var out []*Result
	var firstErr error
	for _, req := range reqs {
		res, err := r.Run(ctx, req)
		if err != nil && res == nil {
			res = &Result{Request: Resolved{Name: req.Name, File: req.File.Path, Line: req.Line, Method: req.Method, URL: req.URL}, Errors: []string{err.Error()}, req: req}
		}
		out = append(out, res)
		if r.OnResult != nil {
			r.OnResult(res, err)
		}
		if err != nil {
			if !r.Opts.KeepGoing {
				return out, err
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !res.OK && !r.Opts.KeepGoing {
			return out, nil
		}
	}
	return out, firstErr
}

// Description is what `apic describe` shows.
type Description struct {
	Name        string            `json:"name,omitempty"`
	ID          string            `json:"id"`
	File        string            `json:"file"`
	Line        int               `json:"line"`
	Description string            `json:"description,omitempty"`
	Method      string            `json:"method"`
	URLTemplate string            `json:"url_template"`
	URL         string            `json:"url"` // resolved as far as possible
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body,omitempty"`
	BodyFile    string            `json:"body_file,omitempty"`
	Variables   []VarInfo         `json:"variables"`
	Captures    []string          `json:"captures,omitempty"`
	Asserts     []string          `json:"asserts,omitempty"`
	Steps       []string          `json:"steps,omitempty"`       // # @step phrases
	Refs        []string          `json:"refs,omitempty"`        // # @ref and # @forceRef targets
	Auth        string            `json:"auth,omitempty"`        // auth spec template
	AuthSource  string            `json:"auth_source,omitempty"` // "request" or "apic.yaml"
	TLS         *TLSInfo          `json:"tls,omitempty"`         // non-default TLS setup for the request's host
	Proxy       *ProxyInfo        `json:"proxy,omitempty"`       // the proxy in effect, when one is configured
	Ready       bool              `json:"ready"`                 // every variable resolves, or a # @ref supplies it
}

// Describe reports a request's variables and where each comes from.
func (r *Runner) Describe(req *httpfile.Request) *Description {
	d := &Description{Name: req.Name, ID: req.ID(), File: req.File.Path, Line: req.Line, Description: req.Description,
		Method: req.Method, URLTemplate: req.URL, Headers: headerMap(req.Headers), Body: req.Body, BodyFile: req.BodyFile, Ready: true}
	seen := map[string]bool{}
	var texts []string
	texts = append(texts, req.URL)
	for _, h := range req.Headers {
		texts = append(texts, h.Value)
	}
	texts = append(texts, req.Body)
	for _, a := range req.Asserts {
		d.Asserts = append(d.Asserts, a.Expr)
		texts = append(texts, a.Expr)
	}
	for _, c := range req.Captures {
		d.Captures = append(d.Captures, c.Name+" = "+c.Selector)
	}
	d.Steps = req.Steps()
	refRuns := map[string]bool{}
	for _, ref := range req.Refs() {
		d.Refs = append(d.Refs, ref.ID)
		if targets, err := r.Project.Resolve(ref.ID); err == nil && len(targets) == 1 {
			refRuns[targets[0].ID()] = true
		}
	}
	if raw, src := r.AuthSource(req); raw != "" {
		d.Auth, d.AuthSource = raw, src
		if spec, err := auth.Parse(raw); err == nil {
			texts = append(texts, spec.Texts()...)
		}
	}
	for _, t := range texts {
		for _, e := range template.Exprs(t) {
			if seen[e] {
				continue
			}
			seen[e] = true
			if strings.HasPrefix(e, "$") || strings.Contains(e, ".response.") {
				v, ok, secret, err := r.resolveExprMeta(req, e, 0)
				info := VarInfo{Name: e, Source: "built-in", Value: v, Secret: secret, Missing: !ok || err != nil}
				if strings.Contains(e, ".response.") {
					info.Source = "response reference (flow only)"
				}
				if err != nil {
					info.Source += ": " + err.Error()
				}
				if !ok || err != nil {
					d.Ready = false
				}
				d.Variables = append(d.Variables, info)
				continue
			}
			info, ok, err := r.lookup(req, e, 0)
			if err != nil {
				info.Source = info.Source + ": " + err.Error()
				info.Missing = true
			}
			if info.Missing && err == nil && refRuns[info.CapturedBy] {
				// A `# @ref` supplies it before the request goes out, so
				// it does not make the request unready.
				info.RefRuns = true
			} else if !ok || err != nil {
				d.Ready = false
			}
			d.Variables = append(d.Variables, info)
		}
	}
	sort.SliceStable(d.Variables, func(i, j int) bool { return d.Variables[i].Missing && !d.Variables[j].Missing })
	d.URL = req.URL
	// Only the host decides the TLS settings, so render the URL alone (as
	// far as it resolves) rather than the whole request.
	rendered, _ := template.Render(strings.TrimSpace(req.URL), func(e string) (string, bool, error) { return r.resolveExpr(req, e) })
	d.TLS = r.tlsInfoForURL(rendered)
	d.Proxy = r.ProxyInfo(rendered)
	return d
}

// tlsInfoForURL reports the non-default TLS setup for a request URL's host.
func (r *Runner) tlsInfoForURL(rawURL string) *TLSInfo {
	u, err := url.Parse(rawURL)
	if err != nil {
		return r.tlsFor("").info()
	}
	return r.tlsFor(u.Host).info()
}

// EnvVar lists the effective variables for the current environment, for `apic env`.
func (r *Runner) EnvVars() []VarInfo {
	names := map[string]bool{}
	for k := range r.Envs.PublicVars(r.Opts.Env) {
		names[k] = true
	}
	for k := range r.Envs.PrivateVars(r.Opts.Env) {
		names[k] = true
	}
	for k := range r.Envs.DotEnv {
		names[k] = true
	}
	if r.Session != nil {
		for k := range r.Session.Vars(r.Opts.Env) {
			if !strings.HasPrefix(k, "$") {
				names[k] = true
			}
		}
	}
	for k := range r.Opts.Vars {
		names[k] = true
	}
	var out []VarInfo
	for n := range names {
		info, _, _ := r.lookup(nil, n, 0)
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// client builds the HTTP client for one request to host.
func (r *Runner) client(req *httpfile.Request, host string) (*http.Client, error) {
	tr, err := r.transport(host)
	if err != nil {
		return nil, err
	}
	c := &http.Client{Transport: tr}
	if _, off := req.Directive("no-cookies"); r.Jar != nil && !off {
		c.Jar = r.Jar.HTTP(r.Opts.Env)
	}
	if _, noRedirect := req.Directive("no-redirect"); noRedirect {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return c, nil
	}
	c.CheckRedirect = stripSensitiveOnCrossHostRedirect
	return c, nil
}

// stripSensitiveOnCrossHostRedirect drops apic's own credential headers when a
// redirect leaves the host that was originally addressed.
//
// net/http already does this for Authorization, Cookie and
// Proxy-Authorization, but its list is fixed and ours is not: the SigV4 signer
// sets X-Amz-Security-Token, "# @auth exec header=..." sets whatever the
// project asks for, and a file can write X-Api-Key: {{secret}} by hand. Those
// would otherwise follow a redirect to any host that answers.
func stripSensitiveOnCrossHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if len(via) == 0 {
		return nil
	}
	if strings.EqualFold(req.URL.Host, via[0].URL.Host) {
		return nil
	}
	for name := range req.Header {
		if sensitiveHeaders[strings.ToLower(name)] {
			req.Header.Del(name)
		}
	}
	return nil
}

func cloneDefaultTransport() *http.Transport {
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		return tr.Clone()
	}
	return &http.Transport{Proxy: http.ProxyFromEnvironment}
}

func statusText(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[i+1:]
	}
	return s
}

func flatHeaders(h http.Header) map[string]string {
	m := map[string]string{}
	for k, v := range h {
		m[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	return m
}

func jsonOrString(data []byte) any {
	t := bytes.TrimSpace(data)
	if len(t) > 0 && json.Valid(t) {
		return json.RawMessage(t)
	}
	return string(data)
}

// readBodyFile reads a `< file` the request refers to (the whole body, or
// one part of a multipart body); what names it in errors.
func (r *Runner) readBodyFile(req *httpfile.Request, rel, what string) ([]byte, error) {
	path, err := r.filePath(req, rel, what)
	if err != nil {
		return nil, err
	}
	// path is resolved and confined to the project root by filePath.
	data, err := os.ReadFile(path) //nolint:gosec // confined to the project root
	if err != nil {
		return nil, usagef("%s:%d: %s: %v", req.File.Path, req.Line, what, err)
	}
	return data, nil
}

// multipartBody assembles a multipart/form-data body from its parts: text
// parts are rendered as templates, `< file` parts read the file (rendered
// too for `<@`). The result is binary-safe and uses the boundary the file
// declares, so the Content-Type header written in the file is right.
func (r *Runner) multipartBody(req *httpfile.Request, m *httpfile.Multipart, render func(string) (string, error)) ([]byte, []FormPart, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.SetBoundary(m.Boundary); err != nil {
		return nil, nil, usagef("%s:%d: multipart boundary %q: %v", req.File.Path, req.Line, m.Boundary, err)
	}
	parts := make([]FormPart, 0, len(m.Parts))
	for _, p := range m.Parts {
		hdr := textproto.MIMEHeader{}
		fp := FormPart{Name: p.Name, Filename: p.Filename, ContentType: p.ContentType}
		for _, h := range p.Headers {
			v, err := render(h.Value)
			if err != nil {
				return nil, nil, usagef("%s:%d: part header %s: %v", req.File.Path, p.Line, h.Name, err)
			}
			hdr.Add(h.Name, v)
			switch {
			case strings.EqualFold(h.Name, "Content-Disposition"):
				if _, params, err := mime.ParseMediaType(v); err == nil {
					fp.Name, fp.Filename = params["name"], params["filename"]
				}
			case strings.EqualFold(h.Name, "Content-Type"):
				fp.ContentType = v
			}
		}
		var data []byte
		if p.File != "" {
			var err error
			if data, err = r.readBodyFile(req, p.File, "part file"); err != nil {
				return nil, nil, err
			}
			if p.FileTemplated {
				s, err := render(string(data))
				if err != nil {
					return nil, nil, usagef("%s:%d: part file %s: %v", req.File.Path, p.FileLine, p.File, err)
				}
				data = []byte(s)
			}
			fp.File = filepath.ToSlash(filepath.Join(filepath.Dir(req.File.Path), p.File))
		} else {
			s, err := render(p.Body)
			if err != nil {
				return nil, nil, usagef("%s:%d: part %s: %v", req.File.Path, p.Line, p.Name, err)
			}
			data, fp.Value = []byte(s), s
		}
		fp.Size = len(data)
		pw, err := w.CreatePart(hdr)
		if err != nil {
			return nil, nil, usagef("%s:%d: part %s: %v", req.File.Path, p.Line, p.Name, err)
		}
		if _, err := pw.Write(data); err != nil {
			return nil, nil, usagef("%s:%d: part %s: %v", req.File.Path, p.Line, p.Name, err)
		}
		parts = append(parts, fp)
	}
	if err := w.Close(); err != nil {
		return nil, nil, usagef("%s:%d: multipart body: %v", req.File.Path, req.Line, err)
	}
	return buf.Bytes(), parts, nil
}

// filePath resolves a `< file` reference relative to the request's file and
// confines it to the project root.
func (r *Runner) filePath(req *httpfile.Request, rel, what string) (string, error) {
	real, err := confine(r.Project.Root, filepath.Join(r.Project.Root, filepath.Dir(req.File.Path)), rel)
	if errors.Is(err, errOutsideRoot) {
		return "", usagef("%s:%d: %s %q resolves outside project root", req.File.Path, req.Line, what, rel)
	}
	if err != nil {
		return "", usagef("%s:%d: %s: %v", req.File.Path, req.Line, what, err)
	}
	return real, nil
}

var errOutsideRoot = errors.New("resolves outside project root")

// confine resolves rel against dir, follows symlinks, and returns the real
// path if it lies under root, else errOutsideRoot.
func confine(root, dir, rel string) (string, error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	joined := rel
	if !filepath.IsAbs(rel) {
		joined = filepath.Join(dir, rel)
	}
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		dirReal, derr := filepath.EvalSymlinks(filepath.Dir(abs))
		if derr != nil {
			real = abs
		} else {
			real = filepath.Join(dirReal, filepath.Base(abs))
		}
	}
	inside, err := filepath.Rel(rootReal, real)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", errOutsideRoot
	}
	return real, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
