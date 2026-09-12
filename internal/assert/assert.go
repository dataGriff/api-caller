// Package assert parses and evaluates `# @assert` expressions of the form
// `<selector> <op> <value>` or `<selector> exists` / `<selector> not exists`.
package assert

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dataGriff/api-caller/internal/selector"
)

// Operators lists the supported comparison operators.
var Operators = []string{"==", "!=", "<=", ">=", "<", ">", "contains", "matches", "startsWith", "endsWith", "exists", "not exists"}

// Expr is a parsed assertion.
type Expr struct {
	Selector string
	Op       string
	Value    string // raw right-hand side; may contain {{placeholders}}
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
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return Expr{}, fmt.Errorf("assert %q: expected `<selector> <op> <value>`", expr)
	}
	sel := fields[0]
	rest := strings.TrimSpace(s[len(sel):])
	if rest == "exists" || rest == "not exists" {
		return Expr{Selector: sel, Op: rest}, nil
	}
	op := fields[1]
	valid := false
	for _, o := range Operators {
		if o == op {
			valid = true
		}
	}
	if !valid {
		return Expr{}, fmt.Errorf("assert %q: unknown operator %q (one of %s)", expr, op, strings.Join(Operators, ", "))
	}
	val := strings.TrimSpace(rest[len(op):])
	if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
		val = val[1 : len(val)-1]
	}
	return Expr{Selector: sel, Op: op, Value: val}, nil
}

// Eval evaluates a parsed expression against a response. expected is the
// right-hand side after template rendering.
func Eval(e Expr, expected string, resp *selector.Response) Result {
	display := e.Selector + " " + e.Op
	if e.Op != "exists" && e.Op != "not exists" {
		display += " " + expected
	}
	res := Result{Expr: display, Expected: expected}
	actual, ok, err := selector.Select(resp, e.Selector)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Actual = actual
	switch e.Op {
	case "exists":
		res.Pass = ok
	case "not exists":
		res.Pass = !ok
	default:
		if !ok {
			res.Error = "no value at " + e.Selector
			return res
		}
		res.Pass, res.Error = compare(actual, e.Op, expected)
	}
	return res
}

func compare(actual, op, expected string) (bool, string) {
	af, aerr := strconv.ParseFloat(actual, 64)
	ef, eerr := strconv.ParseFloat(expected, 64)
	numeric := aerr == nil && eerr == nil
	switch op {
	case "==":
		if numeric {
			return af == ef, ""
		}
		return actual == expected, ""
	case "!=":
		if numeric {
			return af != ef, ""
		}
		return actual != expected, ""
	case "<", "<=", ">", ">=":
		if !numeric {
			return false, fmt.Sprintf("%s needs numeric operands, got %q and %q", op, actual, expected)
		}
		switch op {
		case "<":
			return af < ef, ""
		case "<=":
			return af <= ef, ""
		case ">":
			return af > ef, ""
		default:
			return af >= ef, ""
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
