package selector

import (
	"net/http"
	"testing"
	"time"
)

func TestSelect(t *testing.T) {
	resp := &Response{
		Status: 201, StatusText: "Created",
		Headers:  http.Header{"Content-Type": {"application/json; charset=utf-8"}},
		Body:     []byte(`{"id": 7, "name": "x", "items": [{"id": "a"}, {"id": "b"}], "nested": {"k.y": true}}`),
		Duration: 1500 * time.Millisecond,
	}
	cases := []struct {
		sel, want string
		ok        bool
		err       bool
	}{
		{"status", "201", true, false},
		{"statusText", "Created", true, false},
		{"duration", "1500", true, false},
		{"header.content-type", "application/json; charset=utf-8", true, false},
		{"headers.Content-Type", "application/json; charset=utf-8", true, false},
		{"header.x-missing", "", false, false},
		{"body.$.id", "7", true, false},
		{"body.$.name", "x", true, false},
		{"body.$.items[1].id", "b", true, false},
		{"body.$.items.#", "2", true, false},
		{"body.$.nested[\"k.y\"]", "true", true, false},
		{"body.$.nope", "", false, false},
		{"body.$.items[0]", `{"id": "a"}`, true, false},
		{"bogus", "", false, true},
	}
	for _, c := range cases {
		got, ok, err := Select(resp, c.sel)
		if (err != nil) != c.err || ok != c.ok || got != c.want {
			t.Errorf("%s: got %q ok=%v err=%v; want %q ok=%v err=%v", c.sel, got, ok, err, c.want, c.ok, c.err)
		}
	}
	if _, _, err := Select(&Response{Body: []byte("nope")}, "body.$.x"); err == nil {
		t.Error("want error for non-JSON body")
	}
}
