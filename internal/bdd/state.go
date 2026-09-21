// Package bdd runs Gherkin feature files against a project's .http requests
// with a fixed step vocabulary, using godog embedded in the binary.
package bdd

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
	"github.com/dataGriff/api-caller/internal/session"
)

// Config is shared by every scenario in a run.
type Config struct {
	Project    *project.Project
	Env        string
	Vars       map[string]string
	UseSession bool // read and write .apic/session.json instead of an isolated in-memory session
	Timeout    time.Duration
	Insecure   bool
	Redact     bool
	Cookies    bool // keep a cookie jar (in memory per scenario, shared on disk under UseSession)
	Stderr     io.Writer
	CACert     string // --cacert, --cert and --key
	Cert       string
	Key        string
	Proxy      string // --proxy and --no-proxy
	NoProxy    bool

	usageErr      error           // first usage error raised by a step (unknown environment, request or variable)
	transportErr  error           // first transport error raised by a step
	secrets       map[string]bool // values that must never appear in reports when Redact is set
	stopOnFailure bool            // set from Options.StopOnFailure
	halted        bool            // a scenario failed and --stop-on-failure is on
}

// noteError records the first usage and transport errors so Run can map
// them to exit codes 2 and 3 after the suite finishes.
func (c *Config) noteError(err error) {
	var ue *runner.UsageError
	var te *runner.TransportError
	switch {
	case errors.As(err, &ue):
		if c.usageErr == nil {
			c.usageErr = err
		}
	case errors.As(err, &te):
		if c.transportErr == nil {
			c.transportErr = err
		}
	}
}

// fail masks a step error's text under --redact (error messages may echo
// rendered values), records typed errors for the exit code, and returns
// the error to hand back to godog.
func (c *Config) fail(err error) error {
	if err == nil {
		return nil
	}
	if c.Redact {
		var ue *runner.UsageError
		var te *runner.TransportError
		switch {
		case errors.As(err, &ue):
			err = &runner.UsageError{Msg: c.maskError(ue.Msg)}
		case errors.As(err, &te):
			err = &runner.TransportError{Err: errors.New(c.maskError(te.Err.Error()))}
		default:
			err = errors.New(c.maskError(err.Error()))
		}
	}
	c.noteError(err)
	return err
}

// noteSecrets registers values to mask in redacted reports.
func (c *Config) noteSecrets(vals map[string]string) {
	if !c.Redact {
		return
	}
	if c.secrets == nil {
		c.secrets = map[string]bool{}
	}
	for _, v := range vals {
		if v != "" {
			c.secrets[v] = true
		}
	}
}

// minMaskLen is the shortest secret value masked literally in report text.
// Shorter values (a captured id of "1", say) cannot be masked by
// substitution without corrupting line numbers, counts and JSON in the
// report; they are protected instead by the structural masking of error
// messages, URLs and request values, which never print secret-sourced
// values under --redact.
const minMaskLen = 3

// maskJSON masks secrets inside the string values of a JSON document,
// leaving keys, numbers and structure untouched, so a numeric-looking
// secret cannot corrupt a cucumber report.
func (c *Config) maskJSON(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	v = c.maskValue(v)
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func (c *Config) maskValue(v any) any {
	switch t := v.(type) {
	case string:
		return c.mask(t)
	case []any:
		for i := range t {
			t[i] = c.maskValue(t[i])
		}
		return t
	case map[string]any:
		for k, val := range t {
			if _, structural := cukeStructural[k]; structural {
				if _, isString := val.(string); isString {
					continue
				}
			}
			t[k] = c.maskValue(val)
		}
		return t
	}
	return v
}

// cukeStructural lists cucumber JSON fields whose string values carry
// structure (result statuses, element types, step keywords, locations)
// rather than user text; masking them would change what the report means.
var cukeStructural = map[string]struct{}{"status": {}, "type": {}, "keyword": {}, "uri": {}, "location": {}, "line": {}}

// maskXML masks the text and the user-supplied attributes (name, message)
// of an XML report (JUnit) while leaving its structure untouched.
func (c *Config) maskXML(data []byte) ([]byte, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			el := xml.StartElement{Name: t.Name, Attr: make([]xml.Attr, len(t.Attr))}
			for i, a := range t.Attr {
				v := a.Value
				// Only user text is masked; counts, timings, statuses and
				// types keep the report machine-readable.
				if a.Name.Local == "name" || a.Name.Local == "message" {
					v = c.mask(v)
				}
				el.Attr[i] = xml.Attr{Name: a.Name, Value: v}
			}
			tok = el
		case xml.CharData:
			tok = xml.CharData(c.mask(string(t)))
		default:
			tok = xml.CopyToken(tok)
		}
		if err := enc.EncodeToken(tok); err != nil {
			return nil, err
		}
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// maskError masks an error message: every registered secret regardless of
// length, since a message is short and never parsed, with short values
// replaced only where they stand alone (quoted or between separators).
func (c *Config) maskError(s string) string {
	s = c.mask(s)
	for k := range c.secrets {
		if len(k) >= minMaskLen {
			continue
		}
		re := regexp.MustCompile(`(^|[^\pL\pN])` + regexp.QuoteMeta(k) + `($|[^\pL\pN])`)
		s = re.ReplaceAllString(s, "${1}"+runner.Masked+"${2}")
	}
	return s
}

// mask replaces every registered secret value in s, longest first.
func (c *Config) mask(s string) string {
	if len(c.secrets) == 0 {
		return s
	}
	keys := make([]string, 0, len(c.secrets))
	for k := range c.secrets {
		if len(k) >= minMaskLen {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		s = strings.ReplaceAll(s, k, runner.Masked)
	}
	return s
}

// scenario is the per-scenario state carried in the context.
type scenario struct {
	cfg  *Config
	r    *runner.Runner
	last *runner.Result
}

type ctxKey struct{}

func (c *Config) newScenario(env string) (*scenario, error) {
	return c.scenarioWith(env, nil, nil)
}

// scenarioWith builds a scenario for env, reusing store and jar (the
// previous scenario's session and cookies) when switching environments
// mid-scenario.
func (c *Config) scenarioWith(env string, store *session.Store, jar *session.Jar) (*scenario, error) {
	vars := map[string]string{}
	for k, v := range c.Vars {
		vars[k] = v
	}
	opts := runner.Options{Env: env, Vars: vars, Timeout: c.Timeout, Insecure: c.Insecure, Redact: c.Redact, Cookies: c.Cookies,
		CACert: c.CACert, Cert: c.Cert, Key: c.Key, Proxy: c.Proxy, NoProxy: c.NoProxy}
	if store != nil {
		opts.Session = store
	} else if !c.UseSession {
		opts.Session = session.NewMemory()
	}
	// Each scenario gets its own jar, like its own session; --use-session
	// shares .apic/cookies.json instead.
	if jar != nil {
		opts.CookieJar = jar
	} else if !c.UseSession {
		opts.CookieJar = session.NewMemoryJar()
	}
	r, err := runner.New(c.Project, opts)
	if err != nil {
		return nil, err
	}
	if c.Stderr != nil {
		r.Stderr = c.Stderr
	}
	if c.Redact {
		// r.Opts.Env is the effective environment (apic.yaml may supply the default).
		c.noteSecrets(c.Vars) // --var values are operator input and may be secrets
		c.noteSecrets(r.Envs.PrivateVars(r.Opts.Env))
		c.noteSecrets(r.Envs.DotEnv)
		if r.Session != nil {
			c.noteSecrets(r.Session.Vars(r.Opts.Env))
		}
		// APIC_VAR_* is the documented way to pass CI secrets.
		shell := map[string]string{}
		for _, kv := range os.Environ() {
			if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "APIC_VAR_") {
				shell[k] = v
			}
		}
		c.noteSecrets(shell)
	}
	return &scenario{cfg: c, r: r}, nil
}

func (c *Config) before(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
	if c.halted {
		// --stop-on-failure: later scenarios are recorded as skipped rather
		// than aborting the run, which would leave the report incomplete.
		return ctx, godog.ErrSkip
	}
	sc, err := c.newScenario(c.Env)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, ctxKey{}, sc), nil
}

func (c *Config) after(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
	if err != nil && c.stopOnFailure && !errors.Is(err, godog.ErrSkip) {
		c.halted = true
	}
	return ctx, nil
}

func from(ctx context.Context) (*scenario, error) {
	sc, ok := ctx.Value(ctxKey{}).(*scenario)
	if !ok {
		return nil, fmt.Errorf("no scenario state (internal error)")
	}
	return sc, nil
}

// render substitutes {{variables}} in a step argument.
func (s *scenario) render(text string) (string, error) {
	out, err := s.r.Render(text)
	if err != nil {
		if s.cfg.Redact {
			return "", s.cfg.fail(&runner.UsageError{Msg: "step value (hidden by --redact): " + err.Error()})
		}
		return "", s.cfg.fail(&runner.UsageError{Msg: fmt.Sprintf("%q: %v", text, err)})
	}
	return out, nil
}

// run executes a target (request id or file) and records the last result.
// vars apply to this invocation only: phrase parameters and `with:` tables
// do not leak into later steps.
func (s *scenario) run(ctx context.Context, target string, vars map[string]string) error {
	reqs, err := s.r.Project.Resolve(target)
	if err != nil {
		return s.cfg.fail(&runner.UsageError{Msg: err.Error()})
	}
	return s.runRequests(ctx, reqs, vars)
}

// runRequests runs reqs as a flow with vars scoped to this invocation.
func (s *scenario) runRequests(ctx context.Context, reqs []*httpfile.Request, vars map[string]string) error {
	// Whatever happens next, the previous response is no longer "the
	// response": an assertion after an empty or failed run must not read it.
	s.last = nil
	if len(reqs) == 0 {
		return s.cfg.fail(&runner.UsageError{Msg: "nothing to run: the target has no requests"})
	}
	restore := s.setScoped(vars)
	defer restore()
	results, err := s.r.RunAll(ctx, reqs)
	if len(results) > 0 {
		s.last = results[len(results)-1]
	}
	for _, res := range results {
		s.cfg.noteSecrets(res.Captures)
	}
	if err != nil {
		var te *runner.TransportError
		if s.cfg.Redact && errors.As(err, &te) && s.last != nil {
			// Go's transport errors quote the full URL; keep the masked form only,
			// and record that form so the CLI never prints the original.
			err = &runner.TransportError{Err: fmt.Errorf("could not reach %s %s (details hidden by --redact)", s.last.Request.Method, s.cfg.maskError(s.last.Request.DisplayURL(true)))}
		}
		return s.cfg.fail(err)
	}
	for _, res := range results {
		if !res.OK {
			id := res.Request.Name
			if id == "" {
				id = fmt.Sprintf("%s:%d", res.Request.File, res.Request.Line)
			}
			return s.cfg.fail(fmt.Errorf("%s failed:\n%s", id, s.cfg.describeFailure(res)))
		}
	}
	return nil
}

// setScoped applies vars at --var precedence and returns a function that
// restores the previous values.
func (s *scenario) setScoped(vars map[string]string) func() {
	type prev struct {
		val string
		ok  bool
	}
	saved := map[string]prev{}
	for k, v := range vars {
		old, ok := s.r.Opts.Vars[k]
		saved[k] = prev{old, ok}
		s.r.SetVar(k, v)
	}
	return func() {
		for k, p := range saved {
			if p.ok {
				s.r.Opts.Vars[k] = p.val
			} else {
				delete(s.r.Opts.Vars, k)
			}
		}
	}
}

func (s *scenario) requireLast() (*runner.Result, error) {
	if s.last == nil || s.last.Raw() == nil {
		return nil, fmt.Errorf("no response yet: run a request first (e.g. `When I run \"get-user\"`)")
	}
	return s.last, nil
}

// describeFailure summarises a result for a step error message.
func (c *Config) describeFailure(res *runner.Result) string {
	var b strings.Builder
	url := res.Request.DisplayURL(res.Redact)
	if res.Redact {
		url = c.maskError(url) // DisplayURL masks query values; secrets may sit in the path too
	}
	fmt.Fprintf(&b, "  %s %s\n", res.Request.Method, url)
	if res.Response != nil {
		fmt.Fprintf(&b, "  %d %s (%d ms)\n", res.Response.Status, res.Response.StatusText, res.Response.DurationMs)
	}
	for _, a := range res.Asserts {
		switch {
		case res.Redact && (a.Error != "" || !a.Pass):
			fmt.Fprintf(&b, "  ✗ %s\n", redactExpr(a.Expr))
		case a.Error != "":
			fmt.Fprintf(&b, "  ✗ %s (%s)\n", a.Expr, a.Error)
		case !a.Pass:
			fmt.Fprintf(&b, "  ✗ %s (actual: %s)\n", a.Expr, excerpt(a.Actual, 120))
		}
	}
	if res.Redact {
		if len(res.Errors) > 0 {
			fmt.Fprintf(&b, "  ✗ %d error(s) (details hidden by --redact)\n", len(res.Errors))
		}
		return b.String()
	}
	for _, e := range res.Errors {
		fmt.Fprintf(&b, "  ✗ %s\n", e)
	}
	if raw := res.Raw(); raw != nil && len(raw.Body) > 0 {
		fmt.Fprintf(&b, "  body: %s", excerpt(string(raw.Body), 300))
	}
	return b.String()
}

// redactExpr keeps the selector and operator of an assertion expression
// and hides the expected value.
func redactExpr(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) <= 2 {
		return expr
	}
	return fields[0] + " " + fields[1] + " " + runner.Masked
}

func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// tableRow is one `name | value` line of a step table, kept in file order
// so a value may refer to the rows above it.
type tableRow struct {
	name, value string
}

// tableVars reads a two-column table (with or without a header row) in row
// order; a later row may reference an earlier one with {{name}}.
func tableVars(t *godog.Table) ([]tableRow, error) {
	var out []tableRow
	for i, row := range t.Rows {
		if len(row.Cells) != 2 {
			return nil, &runner.UsageError{Msg: fmt.Sprintf("table row %d must have two cells: name | value", i+1)}
		}
		k, v := row.Cells[0].Value, row.Cells[1].Value
		if i == 0 && strings.EqualFold(k, "name") && strings.EqualFold(v, "value") {
			continue
		}
		if err := checkVarName(k); err != nil {
			return nil, fmt.Errorf("table row %d: %w", i+1, err)
		}
		out = append(out, tableRow{k, v})
	}
	return out, nil
}

// renderTable renders rows top to bottom, each one seeing the rows above it
// at --var precedence, and returns the values plus a function that undoes
// that scoping (in reverse order, so a repeated name restores correctly).
func (s *scenario) renderTable(rows []tableRow) (map[string]string, func(), error) {
	vars := make(map[string]string, len(rows))
	var undo []func()
	restore := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	for _, row := range rows {
		v, err := s.render(row.value)
		if err != nil {
			restore()
			return nil, nil, err
		}
		vars[row.name] = v
		undo = append(undo, s.setScoped(map[string]string{row.name: v}))
	}
	return vars, restore, nil
}

var reVarName = regexp.MustCompile(`^[A-Za-z_][\w.-]*$`)

// checkVarName applies the same rule as `# @capture` names so every value
// set from a feature can be referenced as {{name}}.
func checkVarName(name string) error {
	if !reVarName.MatchString(name) {
		return &runner.UsageError{Msg: fmt.Sprintf("invalid variable name %q: letters, digits, underscore, dot and dash, starting with a letter or underscore", name)}
	}
	if strings.Contains(name, ".response.") {
		// {{name.response.…}} always means a named response, so such a
		// variable could never be read back.
		return &runner.UsageError{Msg: fmt.Sprintf("invalid variable name %q: \".response.\" is reserved for response references", name)}
	}
	return nil
}
