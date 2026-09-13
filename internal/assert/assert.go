// Package assert parses and evaluates `# @assert` expressions of the form
// `<selector> <op> <value>` or `<selector> exists` / `<selector> not exists`.
package assert

import (
	"fmt"
	"math/big"
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
		return Expr{}, fmt.Errorf("assert %q: unknown operator %q (one of %s)", expr, op, strings.Join(Operators, ", "))
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
		if len(digits) > 6 { // ±4096 needs four digits; anything longer is out of range anyway
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
