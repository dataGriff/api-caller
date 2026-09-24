package assert

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/selector"
)

func TestParseAndEval(t *testing.T) {
	resp := &selector.Response{
		Status:  200,
		Headers: http.Header{"Content-Type": {"application/json"}},
		Body:    []byte(`{"id": 7, "name": "alice", "tags": ["a"]}`),
	}
	cases := []struct {
		expr string
		pass bool
	}{
		{"status == 200", true},
		{"status != 200", false},
		{"status < 300", true},
		{"status >= 201", false},
		{"body.$.id == 7.0", true},
		{"body.$.name == alice", true},
		{"body.$.name == \"alice\"", true},
		{"body.$.name contains lic", true},
		{"body.$.name startsWith al", true},
		{"body.$.name endsWith ce", true},
		{"body.$.name matches ^a.*e$", true},
		{"header.content-type contains json", true},
		{"body.$.tags exists", true},
		{"body.$.missing exists", false},
		{"body.$.missing not exists", true},
		{"body.$.missing == 1", false},
		{"body.$.name < 3", false},
	}
	for _, c := range cases {
		e, err := Parse(c.expr)
		if err != nil {
			t.Errorf("%s: %v", c.expr, err)
			continue
		}
		r := Eval(e, e.Value, resp)
		if r.Pass != c.pass {
			t.Errorf("%s: pass=%v (%+v)", c.expr, r.Pass, r)
		}
	}
	for _, bad := range []string{"status", "status ~= 1", "", "status ==", "status exists extra", "status not exists extra"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func TestCompareNumbersExactly(t *testing.T) {
	cases := []struct {
		actual, op, expected string
		want                 bool
	}{
		{"9007199254740993", "==", "9007199254740992", false},
		{"9007199254740993", "!=", "9007199254740992", true},
		{"9007199254740993", ">", "9007199254740992", true},
		{"1.0", "==", "1", true},
		{"1e2", "==", "100", true},
		{"99", "<", "1e2", true},
		{"abc", "==", "abc", true},
		{"1/2", "==", "0.5", false},
		{"1e1000", ">", "1e999", true},
		{"1e-1000", "<", "1", true},
		{"-0.5", "<", "+.5", true},
	}
	for _, c := range cases {
		if got, why := compare(c.actual, c.op, c.expected); got != c.want || why != "" {
			t.Errorf("%s %s %s: got %v (%s), want %v", c.actual, c.op, c.expected, got, why, c.want)
		}
	}
	if _, why := compare("Inf", "<", "5"); why == "" {
		t.Error("Inf is not an exact number and must not compare numerically")
	}
	// Untrusted values with absurd exponents or lengths are not expanded.
	for _, huge := range []string{"1e1000000000", "1e-1000000000", "1" + strings.Repeat("0", 5000)} {
		if _, ok := ParseNumber(huge); ok {
			t.Errorf("%.20s… must not be parsed as an exact number", huge)
		}
		if _, why := compare(huge, "<", "5"); why == "" {
			t.Errorf("%.20s… must not compare numerically", huge)
		}
		if ok, _ := compare(huge, "==", huge); !ok {
			t.Errorf("%.20s… still compares as text", huge)
		}
	}
	if _, ok := ParseNumber("1e4096"); !ok {
		t.Error("an exponent within the bound is parsed")
	}
	if _, ok := ParseNumber("1e1" + strings.Repeat("0", 50)); ok {
		t.Error("an over-long exponent is rejected before it is converted")
	}
	if _, ok := ParseNumber("1e" + strings.Repeat("0", 50) + "1"); !ok {
		t.Error("leading zeroes never make an exponent over-long")
	}
	if _, ok := ParseNumber("1e+0004096"); !ok {
		t.Error("leading zeroes do not count towards the exponent bound")
	}
	if ok, _ := compare("1e+0004096", "==", "1e4096"); !ok {
		t.Error("1e+0004096 and 1e4096 are the same number")
	}
	if _, ok := ParseNumber("1e-0000000"); !ok {
		t.Error("an all-zero exponent is fine")
	}
}

// A selector with spaces inside a bracket is one selector.
func TestParseFilterSelector(t *testing.T) {
	e, err := Parse(`body.$.items[?(@.name == "a b")].length == 2`)
	if err != nil || e.Selector != `body.$.items[?(@.name == "a b")].length` || e.Op != "==" || e.Value != "2" {
		t.Fatalf("%+v %v", e, err)
	}
	e, err = Parse(`body.$.items[?(@.x =~ /a ]b/)] exists`)
	if err != nil || e.Selector != `body.$.items[?(@.x =~ /a ]b/)]` || e.Op != "exists" {
		t.Fatalf("%+v %v", e, err)
	}
}

func TestPredicatesAndLength(t *testing.T) {
	resp := &selector.Response{Status: 200, StatusText: "OK", Body: []byte(`{"id": 7, "ratio": 0.5, "big": 1e3, "name": "zoë", "blank": "", "ok": true, "none": null, "items": [1, 2, 3], "empty": [], "obj": {"a": 1}, "nothing": {}}`)}
	cases := []struct {
		expr   string
		pass   bool
		errHas string
		actual string
	}{
		{"body.$.id isInteger", true, "", "number"},
		{"body.$.id isNumber", true, "", "number"},
		{"body.$.ratio isInteger", false, "", "number"},
		{"body.$.ratio not isInteger", true, "", "number"},
		{"body.$.big isInteger", true, "", "number"},
		{"body.$.name isString", true, "", "string"},
		{"body.$.id isString", false, "", "number"},
		{"body.$.ok isBoolean", true, "", "boolean"},
		{"body.$.none isNull", true, "", "null"},
		{"body.$.none not isNull", false, "", "null"},
		{"body.$.items isArray", true, "", "array"},
		{"body.$.obj isObject", true, "", "object"},
		{"body.$.items not isObject", true, "", "array"},
		{"status isNumber", true, "", "number"},
		{"statusText isString", true, "", "string"},
		{"body.$.blank isEmpty", true, "", ""},
		{"body.$.empty isEmpty", true, "", "[]"},
		{"body.$.nothing isEmpty", true, "", "{}"},
		{"body.$.items not isEmpty", true, "", "[1, 2, 3]"},
		{"body.$.id isEmpty", false, "isEmpty needs a string, an array or an object, got number 7", "7"},
		{"body.$.missing isString", false, "no value at body.$.missing", ""},
		{"body.$.items length == 3", true, "", "3"},
		{"body.$.items length >= 4", false, "", "3"},
		{"body.$.name length == 3", true, "", "3"},
		{"body.$.obj length == 1", true, "", "1"},
		{"body.$.empty length == 0", true, "", "0"},
		{"body.$.id length == 1", false, "length needs a string, an array or an object, got number 7", "7"},
		{"body.$.items[?(@ > 1)] length == 2", true, "", "2"},
	}
	for _, c := range cases {
		e, err := Parse(c.expr)
		if err != nil {
			t.Errorf("%s: %v", c.expr, err)
			continue
		}
		r := Eval(e, e.Value, resp)
		if r.Pass != c.pass || (c.errHas == "" && r.Error != "") || (c.errHas != "" && !strings.Contains(r.Error, c.errHas)) || r.Actual != c.actual {
			t.Errorf("%s: pass=%v err=%q actual=%q; want pass=%v err~%q actual=%q", c.expr, r.Pass, r.Error, r.Actual, c.pass, c.errHas, c.actual)
		}
		if r.Expr != c.expr {
			t.Errorf("%s: displayed as %q", c.expr, r.Expr)
		}
	}
	for expr, want := range map[string]string{
		"body.$.id isString 1":     "does not take a value",
		"body.$.id not isString x": "does not take a value",
		"body.$.items length":      "length <op> <number>",
		"body.$.items length ~= 3": "length takes one of",
		"body.$.items isnt":        "unknown operator",
		"body.$ matchesSchema":     "expected `<selector> <op> <value>`",
	} {
		if _, err := Parse(expr); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v; want %q", expr, err, want)
		}
	}
	// Redaction keeps the operator words and a filter whole.
	for expr, want := range map[string]string{
		"body.$.items length == 3":                "body.$.items length == ***",
		"body.$.id not isString":                  "body.$.id not isString",
		`body.$.items[?(@.a == "x y")].id == abc`: `body.$.items[?(@.a == "x y")].id == ***`,
		"body.$ matchesSchema ./s.json":           "body.$ matchesSchema ***",
		// What Parse cannot read is masked after two words, never shown.
		"body.$.user name == s3cret": "body.$.user name ***",
		"body.$.items[0 == s3cret":   "body.$.items[0 == ***",
		"body.$.x s3cret":            "body.$.x ***",
		"body.$.x":                   "body.$.x",
	} {
		if got := Redact(expr, "***"); got != want {
			t.Errorf("Redact(%s) = %q; want %q", expr, got, want)
		}
	}
}

func TestMatchesSchema(t *testing.T) {
	schema := `{
	  "$schema": "https://json-schema.org/draft/2020-12/schema",
	  "type": "object",
	  "required": ["id", "name"],
	  "properties": {
	    "id": {"type": "integer"},
	    "name": {"type": "string", "minLength": 1},
	    "tags": {"type": "array", "items": {"$ref": "#/$defs/tag"}}
	  },
	  "$defs": {"tag": {"type": "string"}}
	}`
	load := func(path string) ([]byte, error) {
		switch path {
		case "user.json":
			return []byte(schema), nil
		case "broken.json":
			return []byte("{nope"), nil
		}
		return nil, fmt.Errorf("%s: no such file", path)
	}
	reads := 0
	opts := Options{Schema: Schemas(func(path string) ([]byte, error) { reads++; return load(path) })}
	eval := func(body, expr string) Result {
		e, err := Parse(expr)
		if err != nil {
			t.Fatal(err)
		}
		return EvalWith(e, e.Value, &selector.Response{Body: []byte(body)}, opts)
	}
	if r := eval(`{"id": 1, "name": "a", "tags": ["x"]}`, "body.$ matchesSchema user.json"); !r.Pass || r.Error != "" {
		t.Fatalf("valid: %+v", r)
	}
	r := eval(`{"id": "1", "name": "a"}`, "body.$ matchesSchema user.json")
	if r.Pass || r.Error != "" || !strings.Contains(r.Actual, "id") {
		t.Fatalf("wrong type: %+v", r)
	}
	r = eval(`{"id": 1, "name": "a", "tags": [3]}`, "body.$ matchesSchema user.json")
	if r.Pass || !strings.Contains(r.Actual, "tags") {
		t.Fatalf("$ref: %+v", r)
	}
	if r := eval(`{"user": {"id": 1, "name": "a"}}`, "body.$.user matchesSchema user.json"); !r.Pass {
		t.Fatalf("a path into the body: %+v", r)
	}
	if r := eval(`{"id": 1, "name": "a"}`, "body matchesSchema user.json"); !r.Pass {
		t.Fatalf("the raw body is read as JSON: %+v", r)
	}
	if reads != 1 {
		t.Fatalf("user.json was read %d times; a loader resolves each file once", reads)
	}
	if r := eval(`{}`, "body.$ matchesSchema missing.json"); r.Error == "" || !strings.Contains(r.Error, "no such file") {
		t.Fatalf("missing: %+v", r)
	}
	for range 2 {
		if r := eval(`{}`, "body.$ matchesSchema broken.json"); r.Error == "" || !strings.Contains(r.Error, "schema broken.json") {
			t.Fatalf("broken: %+v", r)
		}
	}
	if reads != 3 {
		t.Fatalf("%d reads; a file that fails is not read again either", reads)
	}
	e, _ := Parse("body.$ matchesSchema user.json")
	if r := Eval(e, e.Value, &selector.Response{Body: []byte(`{}`)}); r.Error == "" {
		t.Fatal("without a loader the operator refuses")
	}
}
