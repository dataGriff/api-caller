package assert

import (
	"net/http"
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
	for _, bad := range []string{"status", "status ~= 1", ""} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}
