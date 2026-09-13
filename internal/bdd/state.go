// Package bdd runs Gherkin feature files against a project's .http requests
// with a fixed step vocabulary, using godog embedded in the binary.
package bdd

import (
	"context"
	"fmt"
	"io"
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
		return "", fmt.Errorf("%q: %w", text, err)
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
		return err
	}
	results, err := s.r.RunAll(ctx, reqs)
	if len(results) > 0 {
		s.last = results[len(results)-1]
	}
	if err != nil {
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
		case a.Error != "":
			fmt.Fprintf(&b, "  ✗ %s (%s)\n", a.Expr, a.Error)
		case !a.Pass:
			fmt.Fprintf(&b, "  ✗ %s (actual: %s)\n", a.Expr, excerpt(a.Actual, 120))
		}
	}
	for _, e := range res.Errors {
		fmt.Fprintf(&b, "  ✗ %s\n", e)
	}
	if raw := res.Raw(); raw != nil && len(raw.Body) > 0 && !res.Redact {
		fmt.Fprintf(&b, "  body: %s", excerpt(string(raw.Body), 300))
	}
	return b.String()
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
