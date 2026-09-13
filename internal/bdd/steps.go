package bdd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cucumber/godog"

	"github.com/dataGriff/api-caller/internal/assert"
	"github.com/dataGriff/api-caller/internal/phrase"
	"github.com/dataGriff/api-caller/internal/runner"
)

// Vocabulary documents the built-in steps for `apic test --steps` and the docs.
var Vocabulary = []struct{ Pattern, Purpose string }{
	{`the environment is "<name>"`, "switch the scenario to another environment"},
	{`the variable "<name>" is "<value>"`, "set a variable for later steps"},
	{`the variables:` + " (table name | value)", "set several variables"},
	{`I run "<request>"`, "send a request by id; fails if its # @assert or # @capture fail"},
	{`I run "<request>" with:` + " (table name | value)", "send a request with variables"},
	{`I run the file "<file.http>"`, "send every request in a file, stopping at the first failure"},
	{`the response status is <n>` + " / is not <n>", "status code"},
	{`the response is successful` + " / a client error / a server error", "2xx / 4xx / 5xx"},
	{`the response body "<path>" is "<value>"`, `also: is not, contains, starts with, ends with, matches; <path> like $.items[0].id`},
	{`the response header "<name>" is "<value>"`, "same operators as for the body"},
	{`the response body "<path>" exists` + " / does not exist", "presence of a value"},
	{`the response body is:` + " (doc string)", "semantic JSON equality"},
	{`the response body contains:` + " (doc string)", "JSON subset match"},
	{`the response time is under <n> ms`, "round-trip time"},
	{`I capture the response body "<path>" as "<name>"`, "store a value; also: header"},
}

var opWords = map[string]string{
	"is": "==", "equals": "==", "is not": "!=", "contains": "contains",
	"starts with": "startsWith", "ends with": "endsWith", "matches": "matches",
}

// handlers binds the built-in vocabulary (patterns in package phrase) to
// their implementations.
var handlers = map[string]any{
	"environment":   stepEnvironment,
	"variable":      stepVariable,
	"variables":     stepVariables,
	"run":           stepRun,
	"run-with":      stepRunWith,
	"run-file":      stepRunFile,
	"status":        stepStatus,
	"status-not":    stepStatusNot,
	"status-class":  stepStatusClass,
	"compare":       stepCompare,
	"exists":        stepExists,
	"body-equals":   stepBodyEquals,
	"body-contains": stepBodyContains,
	"duration":      stepDuration,
	"capture":       stepCapture,
}

func registerSteps(sc *godog.ScenarioContext) {
	for _, b := range phrase.Builtin {
		h, ok := handlers[b.Name]
		if !ok {
			panic("no handler for built-in step " + b.Name)
		}
		sc.Step(b.Regex, h)
	}
}

func stepEnvironment(ctx context.Context, env string) (context.Context, error) {
	sc, err := from(ctx)
	if err != nil {
		return ctx, err
	}
	if env, err = sc.render(env); err != nil {
		return ctx, err
	}
	next, err := sc.cfg.scenarioWith(env, sc.r.Session)
	if err != nil {
		return ctx, sc.cfg.fail(err)
	}
	// The scenario's state survives the switch: variables set by steps,
	// what the session held for the previous environment, values captured
	// so far and the last response. Session values are keyed by
	// environment, so they are carried as captures of this scenario.
	for k, v := range sc.r.Opts.Vars {
		next.r.SetVar(k, v)
	}
	if sc.r.Session != nil {
		cache := map[string]string{}
		for k, v := range sc.r.Session.Vars(sc.r.Opts.Env) {
			if strings.HasPrefix(k, "$") {
				// Auth caches ($oauth2:, $exec:) are read from the session
				// under the current environment, not from captures.
				cache[k] = v
				continue
			}
			next.r.Capture(k, v)
		}
		if len(cache) > 0 {
			next.r.Session.Set(next.r.Opts.Env, cache)
			if err := next.r.Session.Save(); err != nil { // a no-op for the in-memory store
				return ctx, sc.cfg.fail(fmt.Errorf("session: %w", err))
			}
		}
	}
	for k, v := range sc.r.Captured() {
		next.r.Capture(k, v)
	}
	for k, v := range sc.r.Results() {
		next.r.SetResult(k, v) // keeps {{name.response...}} references working
	}
	next.last = sc.last
	return context.WithValue(ctx, ctxKey{}, next), nil
}

func stepVariable(ctx context.Context, name, value string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	if err := checkVarName(name); err != nil {
		return sc.cfg.fail(err)
	}
	v, err := sc.render(value)
	if err != nil {
		return err
	}
	sc.r.SetVar(name, v)
	return nil
}

func stepVariables(ctx context.Context, t *godog.Table) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	vars, err := tableVars(t)
	if err != nil {
		return sc.cfg.fail(err)
	}
	for k, v := range vars {
		rv, err := sc.render(v)
		if err != nil {
			return err
		}
		sc.r.SetVar(k, rv)
	}
	return nil
}

func stepRun(ctx context.Context, target string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	if target, err = sc.render(target); err != nil {
		return err
	}
	return sc.run(ctx, target, nil)
}

// stepRunFile runs a whole .http file; a request id or a `file#fragment`
// target is refused so the step means what it says.
func stepRunFile(ctx context.Context, target string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	if target, err = sc.render(target); err != nil {
		return err
	}
	lower := strings.ToLower(target)
	isFile := strings.HasSuffix(lower, ".http") || strings.HasSuffix(lower, ".rest")
	if strings.Contains(target, "#") || !isFile {
		return sc.cfg.fail(&runner.UsageError{Msg: fmt.Sprintf("I run the file: %q is not a .http/.rest file (use `I run %q` for a single request)", target, target)})
	}
	return sc.run(ctx, target, nil)
}

func stepRunWith(ctx context.Context, target string, t *godog.Table) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	vars, err := tableVars(t)
	if err != nil {
		return sc.cfg.fail(err)
	}
	for k, v := range vars {
		if vars[k], err = sc.render(v); err != nil {
			return err
		}
	}
	// The table may supply the target itself, so apply it before rendering.
	restore := sc.setScoped(vars)
	target, err = sc.render(target)
	restore()
	if err != nil {
		return err
	}
	return sc.run(ctx, target, vars)
}

func stepStatus(ctx context.Context, code int) error {
	return check(ctx, "status", "==", strconv.Itoa(code))
}

func stepStatusNot(ctx context.Context, code int) error {
	return check(ctx, "status", "!=", strconv.Itoa(code))
}

func stepStatusClass(ctx context.Context, class string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	res, err := sc.requireLast()
	if err != nil {
		return err
	}
	st := res.Raw().Status
	var ok bool
	switch class {
	case "successful":
		ok = st >= 200 && st < 300
	case "a client error":
		ok = st >= 400 && st < 500
	case "a server error":
		ok = st >= 500 && st < 600
	}
	if !ok {
		return sc.cfg.fail(fmt.Errorf("expected the response to be %s, got %d %s\n%s", class, st, res.Raw().StatusText, sc.cfg.describeFailure(res)))
	}
	return nil
}

func stepCompare(ctx context.Context, where, sel, word, value string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	if sel, err = sc.render(sel); err != nil {
		return err
	}
	expected, err := sc.render(value)
	if err != nil {
		return err
	}
	return check(ctx, selector(where, sel), opWords[word], expected)
}

func stepExists(ctx context.Context, where, sel, word string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	if sel, err = sc.render(sel); err != nil {
		return err
	}
	op := "exists"
	if word == "does not exist" {
		op = "not exists"
	}
	return check(ctx, selector(where, sel), op, "")
}

func stepDuration(ctx context.Context, ms int) error {
	return check(ctx, "duration", "<", strconv.Itoa(ms))
}

func stepCapture(ctx context.Context, where, sel, name string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	if err := checkVarName(name); err != nil {
		return sc.cfg.fail(err)
	}
	res, err := sc.requireLast()
	if err != nil {
		return err
	}
	if sel, err = sc.render(sel); err != nil {
		return err
	}
	r := assert.Eval(assert.Expr{Selector: selector(where, sel), Op: "exists"}, "", res.Raw())
	if r.Error != "" {
		return sc.cfg.fail(fmt.Errorf("capture %s: %s", sel, r.Error))
	}
	if !r.Pass {
		return sc.cfg.fail(fmt.Errorf("capture %s: nothing at %s\n%s", name, selector(where, sel), sc.cfg.describeFailure(res)))
	}
	sc.cfg.noteSecrets(map[string]string{name: r.Actual})
	sc.r.Capture(name, r.Actual) // same precedence as # @capture: below --var, above env files
	// With --use-session the value outlives the scenario, like a request capture.
	if sc.cfg.UseSession && sc.r.Session != nil {
		sc.r.Session.Set(sc.r.Opts.Env, map[string]string{name: r.Actual})
		if err := sc.r.Session.Save(); err != nil {
			return fmt.Errorf("session: %w", err)
		}
	}
	return nil
}

func stepBodyEquals(ctx context.Context, doc *godog.DocString) error {
	return bodyMatch(ctx, doc, true)
}

func stepBodyContains(ctx context.Context, doc *godog.DocString) error {
	return bodyMatch(ctx, doc, false)
}

func bodyMatch(ctx context.Context, doc *godog.DocString, exact bool) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	res, err := sc.requireLast()
	if err != nil {
		return err
	}
	expected, err := sc.render(doc.Content)
	if err != nil {
		return err
	}
	var ok bool
	var why string
	if exact {
		ok, why = jsonEqual(res.Raw().Body, []byte(expected))
	} else {
		ok, why = jsonContains(res.Raw().Body, []byte(expected))
	}
	if !ok {
		if res.Redact {
			return fmt.Errorf("response body does not match the expected document (details hidden by --redact)")
		}
		return sc.cfg.fail(fmt.Errorf("response body mismatch: %s\n%s", why, sc.cfg.describeFailure(res)))
	}
	return nil
}

// check evaluates one assertion against the last response.
func check(ctx context.Context, sel, op, expected string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	res, err := sc.requireLast()
	if err != nil {
		return err
	}
	r := assert.Eval(assert.Expr{Selector: sel, Op: op, Value: expected}, expected, res.Raw())
	if res.Redact {
		if r.Error != "" || !r.Pass {
			return sc.cfg.fail(fmt.Errorf("assertion failed: %s (values hidden by --redact)\n%s", redactExpr(r.Expr), sc.cfg.describeFailure(res)))
		}
		return nil
	}
	if r.Error != "" {
		return sc.cfg.fail(fmt.Errorf("%s: %s\n%s", r.Expr, r.Error, sc.cfg.describeFailure(res)))
	}
	if !r.Pass {
		return sc.cfg.fail(fmt.Errorf("expected %s, got %q\n%s", r.Expr, excerpt(r.Actual, 120), sc.cfg.describeFailure(res)))
	}
	return nil
}

func selector(where, sel string) string {
	sel = phrase.Unquote(strings.TrimSpace(sel))
	if where == "header" {
		return "header." + sel
	}
	switch {
	case sel == "" || sel == "$":
		return "body.$"
	case strings.HasPrefix(sel, "$.") || strings.HasPrefix(sel, "$["):
		return "body." + sel
	case strings.HasPrefix(sel, "["):
		return "body.$" + sel
	default:
		return "body.$." + sel
	}
}
