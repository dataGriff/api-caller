// Package assert parses and evaluates `# @assert` expressions:
// `<selector> <op> <value>`, `<selector> exists` / `not exists`, a
// predicate such as `<selector> isInteger` or `not isEmpty`,
// `<selector> length <op> <n>` and `<selector> matchesSchema <file>`.
package assert

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/dataGriff/api-caller/internal/selector"
)

// Operators lists the supported comparison operators.
var Operators = []string{"==", "!=", "<=", ">=", "<", ">", "contains", "matches", "startsWith", "endsWith", "matchesSchema", "exists", "not exists"}

// Predicates take no value: `body.$.id isInteger`, `body.$.items not
// isEmpty`. Each also has a `not` form.
var Predicates = []string{"isString", "isNumber", "isInteger", "isBoolean", "isArray", "isObject", "isNull", "isEmpty"}

// comparisons are the operators `length` takes: `body.$.items length == 3`.
var comparisons = []string{"==", "!=", "<=", ">=", "<", ">"}

// Expr is a parsed assertion.
type Expr struct {
	Selector string
	// Op is the operator, a predicate (`isString`, `not isEmpty`) or
	// `matchesSchema`, whose Value is the schema file.
	Op    string
	Value string // raw right-hand side; may contain {{placeholders}}
	// Length compares the selected value's length (characters, elements
	// or keys) rather than the value: `body.$.items length == 3`.
	Length bool
}

// Unary reports an operator that takes no value.
func (e Expr) Unary() bool {
	return e.Op == "exists" || e.Op == "not exists" || isPredicate(strings.TrimPrefix(e.Op, "not "))
}

func isPredicate(op string) bool {
	for _, p := range Predicates {
		if p == op {
			return true
		}
	}
	return false
}

// Options carries what evaluation may need beyond the response.
type Options struct {
	// Schema returns the JSON Schema named by `matchesSchema`, read from
	// a file resolved and confined the way the caller's files are; build
	// it with Schemas. Nil refuses the operator.
	Schema func(path string) (*jsonschema.Resolved, error)
}

// Schemas returns a Schema loader that reads each file with read, then
// parses and resolves it once: every later assertion naming the same
// file, and every retry of the request, gets the resolved schema (or the
// same error) without reading it again. It is not safe for concurrent use.
func Schemas(read func(path string) ([]byte, error)) func(path string) (*jsonschema.Resolved, error) {
	type entry struct {
		schema *jsonschema.Resolved
		err    error
	}
	cache := map[string]entry{}
	return func(path string) (*jsonschema.Resolved, error) {
		if e, ok := cache[path]; ok {
			return e.schema, e.err
		}
		schema, err := loadSchema(read, path)
		cache[path] = entry{schema, err}
		return schema, err
	}
}

func loadSchema(read func(string) ([]byte, error), path string) (*jsonschema.Resolved, error) {
	data, err := read(path)
	if err != nil {
		return nil, err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("schema %s: %w", path, err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("schema %s: %w", path, err)
	}
	return resolved, nil
}

// Result is the outcome of evaluating one assertion.
type Result struct {
	Expr     string `json:"expr"`
	Pass     bool   `json:"pass"`
	Actual   string `json:"actual,omitempty"`
	Expected string `json:"expected,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Parse splits an expression into selector, operator and value.
func Parse(expr string) (Expr, error) {
	s := strings.TrimSpace(expr)
	// The selector may hold spaces inside brackets (a filter), so it is
	// read up to the first space outside one.
	sel, rest := selector.Leading(s)
	fields := append([]string{sel}, strings.Fields(rest)...)
	if len(fields) < 2 {
		return Expr{}, fmt.Errorf("assert %q: expected `<selector> <op> <value>`", expr)
	}
	if rest == "exists" || rest == "not exists" {
		return Expr{Selector: sel, Op: rest}, nil
	}
	if unary := strings.Join(fields[1:], " "); isPredicate(strings.TrimPrefix(unary, "not ")) {
		return Expr{Selector: sel, Op: unary}, nil
	}
	if isPredicate(fields[1]) || fields[1] == "not" && len(fields) > 2 && isPredicate(fields[2]) {
		return Expr{}, fmt.Errorf("assert %q: %q does not take a value", expr, strings.Join(fields[1:min(3, len(fields))], " "))
	}
	if fields[1] == "length" {
		if len(fields) < 4 {
			return Expr{}, fmt.Errorf("assert %q: expected `<selector> length <op> <number>`", expr)
		}
		op := fields[2]
		ok := false
		for _, c := range comparisons {
			ok = ok || c == op
		}
		if !ok {
			return Expr{}, fmt.Errorf("assert %q: length takes one of %s, got %q", expr, strings.Join(comparisons, ", "), op)
		}
		val := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(rest, "length")), op))
		return Expr{Selector: sel, Op: op, Value: val, Length: true}, nil
	}
	if strings.HasPrefix(rest, "exists ") {
		return Expr{}, fmt.Errorf("assert %q: %q does not take a value", expr, "exists")
	}
	if strings.HasPrefix(rest, "not exists ") {
		return Expr{}, fmt.Errorf("assert %q: %q does not take a value", expr, "not exists")
	}
	op := fields[1]
	valid := false
	for _, o := range Operators {
		if o == op {
			valid = true
		}
	}
	if !valid {
		return Expr{}, fmt.Errorf("assert %q: unknown operator %q (one of %s, length <op>, or a predicate: %s)", expr, op, strings.Join(Operators, ", "), strings.Join(Predicates, ", "))
	}
	val := strings.TrimSpace(rest[len(op):])
	if val == "" {
		return Expr{}, fmt.Errorf("assert %q: expected `<selector> <op> <value>`", expr)
	}
	if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
		val = val[1 : len(val)-1]
	}
	return Expr{Selector: sel, Op: op, Value: val}, nil
}

// Display is how an expression reads in results: the selector, the
// operator and the rendered value.
func (e Expr) Display(expected string) string {
	op := e.Op
	if e.Length {
		op = "length " + op
	}
	if e.Unary() {
		return e.Selector + " " + op
	}
	return e.Selector + " " + op + " " + expected
}

// Redact keeps an assertion's selector and operator and replaces its
// value with mask. The selector is read the way Parse reads it, so a
// filter with spaces stays whole. What Parse cannot read is masked after
// its first two words, so a secret never shows because the expression
// around it was malformed.
func Redact(expr, mask string) string {
	e, err := Parse(expr)
	if err == nil {
		if e.Unary() {
			return expr
		}
		return e.Display(mask)
	}
	fields := strings.Fields(expr)
	switch len(fields) {
	case 0, 1:
		return expr
	case 2:
		return fields[0] + " " + mask
	}
	return fields[0] + " " + fields[1] + " " + mask
}

// Eval evaluates a parsed expression against a response. expected is the
// right-hand side after template rendering.
func Eval(e Expr, expected string, resp *selector.Response) Result {
	return EvalWith(e, expected, resp, Options{})
}

// EvalWith is Eval with what matchesSchema needs.
func EvalWith(e Expr, expected string, resp *selector.Response, opts Options) Result {
	res := Result{Expr: e.Display(expected)}
	if !e.Unary() {
		res.Expected = expected
	}
	v, ok, err := selector.SelectValue(resp, e.Selector)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Actual = v.Text
	switch e.Op {
	case "exists":
		res.Pass = ok
		return res
	case "not exists":
		res.Pass = !ok
		return res
	}
	if !ok {
		res.Error = "no value at " + e.Selector
		return res
	}
	switch {
	case e.Length:
		n, err := length(v)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Actual = strconv.Itoa(n)
		res.Pass, res.Error = compare(res.Actual, e.Op, expected)
	case e.Unary():
		want := !strings.HasPrefix(e.Op, "not ")
		got, err := predicate(strings.TrimPrefix(e.Op, "not "), v)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		if e.Op != "isEmpty" && e.Op != "not isEmpty" {
			res.Actual = v.Kind.String() // what it is, not what it says
		}
		res.Pass = got == want
	case e.Op == "matchesSchema":
		problem, err := matchesSchema(e.Selector, v, expected, opts)
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Pass = problem == ""
		if !res.Pass {
			res.Actual = problem
		}
	default:
		res.Pass, res.Error = compare(v.Text, e.Op, expected)
	}
	return res
}

// length is a string's characters, an array's elements or an object's keys.
func length(v selector.Value) (int, error) {
	switch v.Kind {
	case selector.KindString:
		return utf8.RuneCountInString(v.Text), nil
	case selector.KindArray:
		var a []json.RawMessage
		if err := json.Unmarshal([]byte(v.Text), &a); err != nil {
			return 0, err
		}
		return len(a), nil
	case selector.KindObject:
		var o map[string]json.RawMessage
		if err := json.Unmarshal([]byte(v.Text), &o); err != nil {
			return 0, err
		}
		return len(o), nil
	}
	return 0, fmt.Errorf("length needs a string, an array or an object, got %s %s", v.Kind, v.Text)
}

func predicate(op string, v selector.Value) (bool, error) {
	switch op {
	case "isString":
		return v.Kind == selector.KindString, nil
	case "isNumber":
		return v.Kind == selector.KindNumber, nil
	case "isInteger":
		if v.Kind != selector.KindNumber {
			return false, nil
		}
		n, ok := ParseNumber(v.Text)
		return ok && n.IsInt(), nil
	case "isBoolean":
		return v.Kind == selector.KindBool, nil
	case "isArray":
		return v.Kind == selector.KindArray, nil
	case "isObject":
		return v.Kind == selector.KindObject, nil
	case "isNull":
		return v.Kind == selector.KindNull, nil
	case "isEmpty":
		if v.Kind != selector.KindString && v.Kind != selector.KindArray && v.Kind != selector.KindObject {
			return false, fmt.Errorf("isEmpty needs a string, an array or an object, got %s %s", v.Kind, v.Text)
		}
		n, err := length(v)
		return n == 0, err
	}
	return false, fmt.Errorf("unknown predicate %s", op)
}

// matchesSchema validates the selected value against a JSON Schema file
// (draft 2020-12 or draft-07, `$ref` within the file). It returns what
// does not match, or "" when it does; err is a schema that cannot be read.
func matchesSchema(sel string, v selector.Value, path string, opts Options) (string, error) {
	if opts.Schema == nil {
		return "", fmt.Errorf("matchesSchema is not available here")
	}
	resolved, err := opts.Schema(path)
	if err != nil {
		return "", err
	}
	var instance any
	text := v.Text
	if v.Kind == selector.KindString && sel != "body" {
		instance = text // a JSON string value, already unquoted
	} else if err := json.Unmarshal([]byte(text), &instance); err != nil {
		return "", fmt.Errorf("matchesSchema needs JSON, and %s is not: %w", sel, err)
	}
	if err := resolved.Validate(instance); err != nil {
		return err.Error(), nil
	}
	return "", nil
}

// reNumber is the decimal syntax assertions treat as numeric: an optional
// sign, digits with an optional fraction, an optional exponent.
var reNumber = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)(?:[eE]([+-]?\d+))?$`)

// Bounds on the digits ParseNumber expands: response values are untrusted,
// and big.Rat materialises 10^exponent, so 1e1000000000 must not be parsed.
const (
	maxNumberDigits   = 4096
	maxNumberExponent = 4096
)

// ParseNumber reads a decimal number exactly, so large integers such as
// 9007199254740993 keep their value instead of rounding through float64,
// and values beyond float64's range (1e1000) still compare as numbers.
// Words, Inf, NaN, fractions like 1/2 and numbers with more than 4096
// digits or an exponent beyond ±4096 are not numbers here and compare as
// text.
func ParseNumber(s string) (*big.Rat, bool) {
	m := reNumber.FindStringSubmatch(s)
	if m == nil || len(m[1]) > maxNumberDigits {
		return nil, false
	}
	if m[2] != "" {
		sign := strings.TrimRight(m[2], "0123456789")
		digits := strings.TrimLeft(strings.TrimPrefix(m[2], sign), "0") // 1e+0004096 is 1e4096
		if len(digits) > 6 {                                            // ±4096 needs four digits; anything longer is out of range anyway
			return nil, false
		}
		if digits == "" {
			digits = "0"
		}
		exp, err := strconv.Atoi(strings.TrimPrefix(sign+digits, "+"))
		if err != nil || exp > maxNumberExponent || exp < -maxNumberExponent {
			return nil, false
		}
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, false
	}
	return r, true
}

func compare(actual, op, expected string) (bool, string) {
	an, aok := ParseNumber(actual)
	en, eok := ParseNumber(expected)
	numeric := aok && eok
	cmp := 0
	if numeric {
		cmp = an.Cmp(en)
	}
	switch op {
	case "==":
		if numeric {
			return cmp == 0, ""
		}
		return actual == expected, ""
	case "!=":
		if numeric {
			return cmp != 0, ""
		}
		return actual != expected, ""
	case "<", "<=", ">", ">=":
		if !numeric {
			return false, fmt.Sprintf("%s needs numeric operands, got %q and %q", op, actual, expected)
		}
		switch op {
		case "<":
			return cmp < 0, ""
		case "<=":
			return cmp <= 0, ""
		case ">":
			return cmp > 0, ""
		default:
			return cmp >= 0, ""
		}
	case "contains":
		return strings.Contains(actual, expected), ""
	case "startsWith":
		return strings.HasPrefix(actual, expected), ""
	case "endsWith":
		return strings.HasSuffix(actual, expected), ""
	case "matches":
		re, err := regexp.Compile(expected)
		if err != nil {
			return false, "bad regex: " + err.Error()
		}
		return re.MatchString(actual), ""
	}
	return false, "unknown operator " + op
}
