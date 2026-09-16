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
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Session   *session.Store    // use this store instead of opening .apic/session.json (tests use session.NewMemory())
}

// Runner executes requests for one project.
type Runner struct {
	Project *project.Project
	Envs    *env.Environments
	Session *session.Store
	Opts    Options
	Stderr  io.Writer // interactive prompts such as device-code sign-in; nil means os.Stderr

	results  map[string]*Result
	captured map[string]string
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
	r := &Runner{Project: p, Envs: envs, Opts: opts, Stderr: os.Stderr, results: map[string]*Result{}, captured: map[string]string{}}
	switch {
	case opts.Session != nil:
		r.Session = opts.Session
	case !opts.NoSession:
		if r.Session, err = session.Open(p.Root); err != nil {
			return nil, usagef("session: %v", err)
		}
	}
	return r, nil
}

// Resolved is a request with every placeholder substituted.
type Resolved struct {
	Name     string            `json:"name,omitempty"`
	File     string            `json:"file"`
	Line     int               `json:"line"`
	Method   string            `json:"method"`
	URL      string            `json:"url"`
	Headers  []httpfile.Header `json:"-"`
	Body     string            `json:"-"`
	Auth     string            `json:"auth,omitempty"` // auth type applied, e.g. "aws"
	AuthSpec *auth.Spec        `json:"-"`              // rendered spec (contains secrets)
	// SecretHeaders names headers whose value came from a secret source
	// (private env file, .env, session or a capture).
	SecretHeaders map[string]bool `json:"-"`
	missing       []string
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
}

// Result is the outcome of running one request.
type Result struct {
	OK       bool              `json:"ok"`
	Request  Resolved          `json:"request"`
	Response *Response         `json:"response,omitempty"`
	Captures map[string]string `json:"captures,omitempty"`
	Asserts  []assert.Result   `json:"asserts,omitempty"`
	Errors   []string          `json:"errors,omitempty"`
	Redact   bool              `json:"-"` // set from Options.Redact
	raw      *selector.Response
}

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
		out, err := template.Render(s, func(e string) (string, bool, error) { return r.resolveExpr(req, e, 0) })
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
		path, err := r.bodyFilePath(req)
		if err != nil {
			return nil, err
		}
		// path is resolved and confined to the project root by bodyFilePath.
		data, err := os.ReadFile(path) //nolint:gosec // confined to the project root
		if err != nil {
			return nil, usagef("%s:%d: body file: %v", req.File.Path, req.Line, err)
		}
		body = string(data)
		if !req.BodyFileTemplated {
			res.Body = body
			body = ""
		}
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

func (r *Runner) authEnv() *auth.Env {
	tr := cloneDefaultTransport()
	if r.Opts.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit --insecure
	}
	e := &auth.Env{
		AllowExec: r.Project.Config.Auth.AllowExec,
		Stderr:    r.Stderr,
		Client:    &http.Client{Timeout: r.Opts.Timeout, Transport: tr},
	}
	if r.Session != nil {
		e.Cache = sessionCache{r}
	}
	return e
}

// Render substitutes {{placeholders}} in arbitrary text using the runner's
// variables (no file-level @vars, since no request is in scope).
func (r *Runner) Render(s string) (string, error) {
	return template.Render(s, func(e string) (string, bool, error) { return r.resolveExpr(nil, e, 0) })
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

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Run resolves, sends and evaluates a single request. A non-nil error is a
// usage or transport problem; assertion failures are reported in Result.OK.
func (r *Runner) Run(ctx context.Context, req *httpfile.Request) (*Result, error) {
	resolved, err := r.Resolve(req)
	if err != nil {
		return nil, err
	}
	if len(resolved.missing) > 0 {
		return nil, r.MissingError(req, resolved.missing)
	}
	result := &Result{Request: *resolved, OK: true, Redact: r.Opts.Redact}
	preparedAsserts := make([]struct {
		expr     assert.Expr
		expected string
		raw      string
	}, 0, len(req.Asserts))
	for _, a := range req.Asserts {
		expr, err := assert.Parse(a.Expr)
		if err != nil {
			return nil, usagef("%s:%d: %v", req.File.Path, a.Line, err)
		}
		expected := expr.Value
		if expr.Op != "exists" && expr.Op != "not exists" {
			expected, err = template.Render(expr.Value, func(e string) (string, bool, error) { return r.resolveExpr(req, e, 0) })
			if err != nil {
				var me *template.MissingError
				if errors.As(err, &me) {
					return nil, r.MissingError(req, dedupe(me.Exprs))
				}
				return nil, usagef("%s:%d: assert %q: %v", req.File.Path, a.Line, a.Expr, err)
			}
		}
		preparedAsserts = append(preparedAsserts, struct {
			expr     assert.Expr
			expected string
			raw      string
		}{expr: expr, expected: expected, raw: a.Expr})
	}

	timeout := r.Opts.Timeout
	if t, ok := req.Directive("timeout"); ok {
		d, err := time.ParseDuration(t)
		if err != nil {
			return nil, usagef("%s:%d: bad @timeout %q", req.File.Path, req.Line, t)
		}
		timeout = d
	}
	client := r.client(req)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.Reader
	if resolved.Body != "" {
		body = strings.NewReader(resolved.Body)
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
		if err := auth.Apply(ctx, resolved.AuthSpec, httpReq, []byte(resolved.Body), r.authEnv()); err != nil {
			return nil, usagef("%s:%d: auth: %v", req.File.Path, req.Line, err)
		}
	}

	start := time.Now()
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	defer func() { _ = httpResp.Body.Close() }()
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	dur := time.Since(start)

	raw := &selector.Response{Status: httpResp.StatusCode, StatusText: statusText(httpResp.Status), Headers: httpResp.Header, Body: data, Duration: dur}
	result.raw = raw
	result.Response = &Response{Status: raw.Status, StatusText: raw.StatusText, Headers: flatHeaders(httpResp.Header), Body: jsonOrString(data), DurationMs: dur.Milliseconds(), Size: len(data)}

	// Captures first so asserts can reference them.
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
		r.captured[c.Name] = v
	}
	if req.Name != "" {
		r.results[req.Name] = result
	}
	for _, a := range preparedAsserts {
		ar := assert.Eval(a.expr, a.expected, raw)
		if !ar.Pass {
			result.OK = false
		}
		result.Asserts = append(result.Asserts, ar)
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
	return result, nil
}

// RunAll runs requests in order as a flow, stopping at the first failure
// unless KeepGoing is set. Results for requests that ran are always returned.
func (r *Runner) RunAll(ctx context.Context, reqs []*httpfile.Request) ([]*Result, error) {
	var out []*Result
	var firstErr error
	for _, req := range reqs {
		res, err := r.Run(ctx, req)
		if err != nil {
			if res == nil {
				res = &Result{Request: Resolved{Name: req.Name, File: req.File.Path, Line: req.Line, Method: req.Method, URL: req.URL}, Errors: []string{err.Error()}}
			}
			out = append(out, res)
			if !r.Opts.KeepGoing {
				return out, err
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, res)
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
	Auth        string            `json:"auth,omitempty"`        // auth spec template
	AuthSource  string            `json:"auth_source,omitempty"` // "request" or "apic.yaml"
	Ready       bool              `json:"ready"`                 // every variable resolves
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
			if !ok || err != nil {
				d.Ready = false
			}
			if err != nil {
				info.Source = info.Source + ": " + err.Error()
				info.Missing = true
			}
			d.Variables = append(d.Variables, info)
		}
	}
	sort.SliceStable(d.Variables, func(i, j int) bool { return d.Variables[i].Missing && !d.Variables[j].Missing })
	d.URL = req.URL
	return d
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

func (r *Runner) client(req *httpfile.Request) *http.Client {
	tr := cloneDefaultTransport()
	if r.Opts.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit --insecure
	}
	c := &http.Client{Transport: tr}
	if _, noRedirect := req.Directive("no-redirect"); noRedirect {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return c
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

func (r *Runner) bodyFilePath(req *httpfile.Request) (string, error) {
	root, err := filepath.EvalSymlinks(r.Project.Root)
	if err != nil {
		return "", usagef("%s:%d: body file: %v", req.File.Path, req.Line, err)
	}
	path := filepath.Join(r.Project.Root, filepath.Dir(req.File.Path), req.BodyFile)
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", usagef("%s:%d: body file: %v", req.File.Path, req.Line, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			dirReal, derr := filepath.EvalSymlinks(filepath.Dir(abs))
			if derr != nil {
				real = abs
			} else {
				real = filepath.Join(dirReal, filepath.Base(abs))
			}
		} else {
			return "", usagef("%s:%d: body file: %v", req.File.Path, req.Line, err)
		}
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", usagef("%s:%d: body file %q resolves outside project root", req.File.Path, req.Line, req.BodyFile)
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
