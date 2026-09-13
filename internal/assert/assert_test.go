package assert

import (
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
	if _, ok := ParseNumber("1e" + strings.Repeat("0", 50) + "1"); ok {
		t.Error("an over-long exponent is rejected before it is converted")
	}
	if _, ok := ParseNumber("1e+0004096"); ok {
		t.Error("an exponent with more digits than the bound allows is rejected")
	}
}
