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
	{`the response body "<path>" is "<value>"`, `also: is not, contains, starts with, ends with, matches; <path> like $.items[0].id, $..id or $.items[?(@.done == true)].id`},
	{`the response header "<name>" is "<value>"`, "same operators as for the body"},
	{`the response cookie "<name>" is "<value>"`, "a cookie the response set; same operators, also exists / does not exist"},
	{`the response body "<path>" exists` + " / does not exist", "presence of a value"},
	{`the response body "<path>" has length <n>`, "characters of a string, elements of an array, keys of an object"},
	{`the response body "<path>" is a number`, "also: a string, an integer, a boolean, an array, an object, null"},
	{`the response body "<path>" is empty` + " / is not empty", "an empty string, array or object"},
	{`the response body matches the schema "<file.json>"`, `JSON Schema (2020-12 or draft-07), path from the project root; also: the response body "<path>" matches the schema "<file.json>"`},
	{`the response body is:` + " (doc string)", "semantic JSON equality"},
	{`the response body contains:` + " (doc string)", "JSON subset match"},
	{`the response time is under <n> ms`, "round-trip time"},
	{`I capture the response body "<path>" as "<name>"`, "store a value; also: header, cookie"},
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
	"length":        stepLength,
	"type":          stepType,
	"empty":         stepEmpty,
	"schema":        stepSchema,
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
	next, err := sc.cfg.scenarioWith(env, sc.r.Session, sc.r.Jar)
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
	rows, err := tableVars(t)
	if err != nil {
		return sc.cfg.fail(err)
	}
	// Rows apply in order so `| url | {{base}}/x |` may follow `| base | … |`.
	for _, row := range rows {
		rv, err := sc.render(row.value)
		if err != nil {
			return err
		}
		sc.r.SetVar(row.name, rv)
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
	// The whole target is a file name first (odd#name.http is a file);
	// only then is a `#` read as a fragment, which this step refuses.
	if f := sc.r.Project.File(target); f != nil {
		return sc.runRequests(ctx, f.Requests, nil)
	}
	lower := strings.ToLower(target)
	isFile := strings.HasSuffix(lower, ".http") || strings.HasSuffix(lower, ".rest")
	if strings.Contains(target, "#") || !isFile {
		return sc.cfg.fail(runner.Usage(runner.CodeFeatures, fmt.Sprintf("I run the file: %q is not a .http/.rest file (use `I run %q` for a single request)", target, target)))
	}
	return sc.run(ctx, target, nil)
}

func stepRunWith(ctx context.Context, target string, t *godog.Table) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	rows, err := tableVars(t)
	if err != nil {
		return sc.cfg.fail(err)
	}
	// Rows render in order, each seeing the ones above it, and the table may
	// supply the target itself, so it stays scoped while the target renders.
	vars, restore, err := sc.renderTable(rows)
	if err != nil {
		return err
	}
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

func stepLength(ctx context.Context, sel string, n int) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	if sel, err = sc.render(sel); err != nil {
		return err
	}
	return checkExpr(ctx, assert.Expr{Selector: selector("body", sel), Op: "==", Length: true}, strconv.Itoa(n))
}

var typePredicates = map[string]string{
	"a string": "isString", "a number": "isNumber", "an integer": "isInteger", "a boolean": "isBoolean",
	"an array": "isArray", "an object": "isObject", "null": "isNull",
}

func stepType(ctx context.Context, sel, kind string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	if sel, err = sc.render(sel); err != nil {
		return err
	}
	return checkExpr(ctx, assert.Expr{Selector: selector("body", sel), Op: typePredicates[kind]}, "")
}

func stepEmpty(ctx context.Context, sel, word string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	if sel, err = sc.render(sel); err != nil {
		return err
	}
	op := "isEmpty"
	if word == "is not empty" {
		op = "not isEmpty"
	}
	return checkExpr(ctx, assert.Expr{Selector: selector("body", sel), Op: op}, "")
}

func stepSchema(ctx context.Context, sel, file string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	if sel, err = sc.render(sel); err != nil {
		return err
	}
	if file, err = sc.render(file); err != nil {
		return err
	}
	return checkExpr(ctx, assert.Expr{Selector: selector("body", sel), Op: "matchesSchema", Value: file}, file)
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
	return checkExpr(ctx, assert.Expr{Selector: sel, Op: op, Value: expected}, expected)
}

// checkExpr evaluates a parsed assertion against the last response; a
// schema it names is read from the project root.
func checkExpr(ctx context.Context, e assert.Expr, expected string) error {
	sc, err := from(ctx)
	if err != nil {
		return err
	}
	res, err := sc.requireLast()
	if err != nil {
		return err
	}
	r := assert.EvalWith(e, expected, res.Raw(), assert.Options{Schema: assert.Schemas(sc.r.ProjectFile)})
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
	if where == "header" || where == "cookie" {
		return where + "." + sel
	}
	switch {
	case sel == "" || sel == "$":
		return "body.$"
	case strings.HasPrefix(sel, "$.") || strings.HasPrefix(sel, "$["):
		return "body." + sel
	case strings.HasPrefix(sel, "[") || strings.HasPrefix(sel, ".."):
		return "body.$" + sel
	default:
		return "body.$." + sel
	}
}
