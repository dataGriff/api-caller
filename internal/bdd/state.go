// Package bdd runs Gherkin feature files against a project's .http requests
// with a fixed step vocabulary, using godog embedded in the binary.
package bdd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/cucumber/godog"

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
	Stderr     io.Writer

	usageErr     error           // first usage error raised by a step (unknown environment, request or variable)
	transportErr error           // first transport error raised by a step
	secrets      map[string]bool // values that must never appear in reports when Redact is set
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
			t[k] = c.maskValue(val)
		}
		return t
	}
	return v
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
	vars := map[string]string{}
	for k, v := range c.Vars {
		vars[k] = v
	}
	opts := runner.Options{Env: env, Vars: vars, Timeout: c.Timeout, Insecure: c.Insecure, Redact: c.Redact}
	if !c.UseSession {
		opts.Session = session.NewMemory()
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
	sc, err := c.newScenario(c.Env)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, ctxKey{}, sc), nil
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
		var uerr *runner.UsageError
		if s.cfg.Redact {
			uerr = &runner.UsageError{Msg: "step value (hidden by --redact): " + err.Error()}
		} else {
			uerr = &runner.UsageError{Msg: fmt.Sprintf("%q: %v", text, err)}
		}
		s.cfg.noteError(uerr)
		return "", uerr
	}
	return out, nil
}

// run executes a target (request id or file) and records the last result.
func (s *scenario) run(ctx context.Context, target string, vars map[string]string) error {
	for k, v := range vars {
		s.r.SetVar(k, v)
	}
	reqs, err := s.r.Project.Resolve(target)
	if err != nil {
		uerr := &runner.UsageError{Msg: err.Error()}
		s.cfg.noteError(uerr)
		return uerr
	}
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
			err = &runner.TransportError{Err: fmt.Errorf("could not reach %s %s (details hidden by --redact)", s.last.Request.Method, s.last.Request.DisplayURL(true))}
		}
		s.cfg.noteError(err)
		return err
	}
	for _, res := range results {
		if !res.OK {
			return fmt.Errorf("%s failed:\n%s", res.Request.Name, describeFailure(res))
		}
	}
	return nil
}

func (s *scenario) requireLast() (*runner.Result, error) {
	if s.last == nil || s.last.Raw() == nil {
		return nil, fmt.Errorf("no response yet: run a request first (e.g. `When I run \"get-user\"`)")
	}
	return s.last, nil
}

// describeFailure summarises a result for a step error message.
func describeFailure(res *runner.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s %s\n", res.Request.Method, res.Request.DisplayURL(res.Redact))
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

// tableVars reads a two-column table (with or without a header row) into a
// map. A header row is detected when the first row is `name | value`.
func tableVars(t *godog.Table) (map[string]string, error) {
	out := map[string]string{}
	for i, row := range t.Rows {
		if len(row.Cells) != 2 {
			return nil, fmt.Errorf("table row %d must have two cells: name | value", i+1)
		}
		k, v := row.Cells[0].Value, row.Cells[1].Value
		if i == 0 && strings.EqualFold(k, "name") && strings.EqualFold(v, "value") {
			continue
		}
		out[k] = v
	}
	return out, nil
}
