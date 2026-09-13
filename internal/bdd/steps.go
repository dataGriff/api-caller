package bdd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cucumber/godog"

	"github.com/dataGriff/api-caller/internal/assert"
	"github.com/dataGriff/api-caller/internal/phrase"
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
	"run-file":      stepRun,
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
	next, err := sc.cfg.newScenario(env)
	if err != nil {
		sc.cfg.noteError(err)
		return ctx, err
	}
	return context.WithValue(ctx, ctxKey{}, next), nil
}

func stepVariable(ctx context.Context, name, value string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
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
		return err
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
	return sc.run(ctx, target, nil)
}

func stepRunWith(ctx context.Context, target string, t *godog.Table) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	vars, err := tableVars(t)
	if err != nil {
		return err
	}
	for k, v := range vars {
		if vars[k], err = sc.render(v); err != nil {
			return err
		}
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
		ok = st >= 500
	}
	if !ok {
		return fmt.Errorf("expected the response to be %s, got %d %s\n%s", class, st, res.Raw().StatusText, describeFailure(res))
	}
	return nil
}

func stepCompare(ctx context.Context, where, sel, word, value string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	expected, err := sc.render(value)
	if err != nil {
		return err
	}
	return check(ctx, selector(where, sel), opWords[word], expected)
}

func stepExists(ctx context.Context, where, sel, word string) error {
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
	res, err := sc.requireLast()
	if err != nil {
		return err
	}
	r := assert.Eval(assert.Expr{Selector: selector(where, sel), Op: "exists"}, "", res.Raw())
	if r.Error != "" {
		return fmt.Errorf("capture %s: %s", sel, r.Error)
	}
	if !r.Pass {
		return fmt.Errorf("capture %s: nothing at %s\n%s", name, selector(where, sel), describeFailure(res))
	}
	sc.cfg.noteSecrets(map[string]string{name: r.Actual})
	sc.r.SetVar(name, r.Actual)
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
		return fmt.Errorf("response body mismatch: %s\n%s", why, describeFailure(res))
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
			return fmt.Errorf("assertion failed: %s (values hidden by --redact)\n%s", redactExpr(r.Expr), describeFailure(res))
		}
		return nil
	}
	if r.Error != "" {
		return fmt.Errorf("%s: %s\n%s", r.Expr, r.Error, describeFailure(res))
	}
	if !r.Pass {
		return fmt.Errorf("expected %s, got %q\n%s", r.Expr, excerpt(r.Actual, 120), describeFailure(res))
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
