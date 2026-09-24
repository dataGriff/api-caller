package selector

import (
	"net/http"
	"strings"
	"testing"
)

const doc = `{
  "id": 7,
  "name": "shop",
  "length": 99,
  "items": [
    {"id": "a", "name": "apple", "done": true, "price": 1.5, "tags": ["red", "fruit"]},
    {"id": "b", "name": "banana", "done": false, "price": 0.25, "tags": []},
    {"id": "c", "name": "Avocado", "done": true, "price": 2, "owner": {"id": "u1"}}
  ],
  "meta": {"k.y": "dot", "a*b": "star", "empty": [], "nothing": null, "count": 3},
  "big": 9007199254740993
}`

// Every form the issue lists, with the value it selects on one document.
func TestPaths(t *testing.T) {
	resp := &Response{Body: []byte(doc)}
	cases := []struct {
		sel, want string
		kind      Kind
		ok        bool
	}{
		// What worked before keeps working, byte for byte.
		{"body.$.id", "7", KindNumber, true},
		{"body.$.name", "shop", KindString, true},
		{"body.$.items[1].id", "b", KindString, true},
		{"body.$.items.#", "3", KindNumber, true},
		{`body.$.meta["k.y"]`, "dot", KindString, true},
		{`body.$.meta['a*b']`, "star", KindString, true},
		{"body.$.items[0].tags", `["red", "fruit"]`, KindArray, true},
		{"body.$.items[2].owner", `{"id": "u1"}`, KindObject, true},
		{"body.$.items.#.id", `["a","b","c"]`, KindArray, true},
		{"body.$.big", "9007199254740993", KindNumber, true},
		{"body.$.meta.nothing", "null", KindNull, true},
		{"body.$.items[0].done", "true", KindBool, true},
		{"body.$.nope", "", KindString, false},
		// Negative indexes and slices.
		{"body.$.items[-1].id", "c", KindString, true},
		{"body.$.items[-3].id", "a", KindString, true},
		{"body.$.items[-4].id", "", KindString, false},
		{"body.$.items[1:3].id", `["b","c"]`, KindArray, true},
		{"body.$.items[:2].id", `["a","b"]`, KindArray, true},
		{"body.$.items[-2:].id", `["b","c"]`, KindArray, true},
		{"body.$.items[5:9]", "", KindString, false},
		{"body.$.items[1:99].id", `["b","c"]`, KindArray, true},
		{"body.$.items[-99:1].id", `["a"]`, KindArray, true},
		// gjson's forms: a numeric key on an array is an index, and a #
		// in the middle maps, so a count after it is each item's own.
		{"body.$.items.0.id", "a", KindString, true},
		{"body.$.items.2.owner.id", "u1", KindString, true},
		{"body.$.items.3.id", "", KindString, false},
		{"body.$.meta.0", "", KindString, false},
		{"body.$.items.#.tags.#", "[2,0]", KindArray, true},
		{"body.$.items.#.name.length", "[5,6,7]", KindArray, true},
		{"body.$.items.#.nope.#", "", KindString, false},
		// Wildcards.
		{"body.$.items[*].name", `["apple","banana","Avocado"]`, KindArray, true},
		{"body.$.items.*.id", `["a","b","c"]`, KindArray, true},
		// Recursive descent, document order.
		{"body.$..id", `[7,"a","b","c","u1"]`, KindArray, true},
		{"body.$.items..id", `["a","b","c","u1"]`, KindArray, true},
		{"body.$..tags[0]", `["red"]`, KindArray, true},
		{"body.$..nope", "", KindString, false},
		// Filters.
		{"body.$.items[?(@.done == true)].id", `["a","c"]`, KindArray, true},
		{"body.$.items[?(@.done==false)].name", `["banana"]`, KindArray, true},
		{"body.$.items[?(@.price > 1)].id", `["a","c"]`, KindArray, true},
		{"body.$.items[?(@.price <= 0.25)].id", `["b"]`, KindArray, true},
		{`body.$.items[?(@.name =~ /^a/)].id`, `["a"]`, KindArray, true},
		{`body.$.items[?(@.name =~ /^a/i)].id`, `["a","c"]`, KindArray, true},
		{`body.$.items[?(@.name =~ /[bc]an/)].id`, `["b"]`, KindArray, true},
		{`body.$.items[?(@.id == "b" || @.id == 'c')].id`, `["b","c"]`, KindArray, true},
		{"body.$.items[?(@.done == true && @.price < 2)].id", `["a"]`, KindArray, true},
		{"body.$.items[?(@.owner)].id", `["c"]`, KindArray, true},
		{"body.$.items[?(!@.owner)].id", `["a","b"]`, KindArray, true},
		{"body.$.items[?(@.owner.id == 'u1')].name", `["Avocado"]`, KindArray, true},
		{"body.$.items[?(@.tags.length == 2)].id", `["a"]`, KindArray, true},
		{"body.$.items[?@.done == true].id", `["a","c"]`, KindArray, true},
		{"body.$.items[?(@.price == 9)]", "", KindString, false},
		// Brackets inside a filter nest, and a ] in quotes is text.
		{`body.$.items[?(@.tags[0] == "red")].id`, `["a"]`, KindArray, true},
		{`body.$.items[?(@['id'] == "b")].name`, `["banana"]`, KindArray, true},
		{`body.$.items[?(@.tags[?(@ == "fruit")])].id`, `["a"]`, KindArray, true},
		{`body.$.items[?(@.name == "a]")]`, "", KindString, false},
		// != holds where the key is missing (RFC 9535); nothing else does.
		{`body.$.items[?(@.owner.id != "u1")].id`, `["a","b"]`, KindArray, true},
		{`body.$.items[?(@.owner.id == "u2")]`, "", KindString, false},
		{`body.$.items[?(@.owner.id < "z")].id`, `["c"]`, KindArray, true},
		// In a filter @.length on an object is its key, never a key count.
		{"body.$.items[?(@.length)]", "", KindString, false},
		{"body.$.items[?(!@.length)].id", `["a","b","c"]`, KindArray, true},
		{"body.$.items[?(@.name.length > 5)].id", `["b","c"]`, KindArray, true},
		// Counting: an array, an object, a string, and a set of matches.
		{"body.$.items.length", "3", KindNumber, true},
		{"body.$.meta.#", "5", KindNumber, true},
		{"body.$.items[0].name.length", "5", KindNumber, true},
		{"body.$.items[?(@.done == true)].length", "2", KindNumber, true},
		{"body.$.items[?(@.price == 9)].#", "0", KindNumber, true},
		{"body.$..id.#", "5", KindNumber, true},
		{"body.$.meta.empty.length", "0", KindNumber, true},
		// A number, boolean or null has no count.
		{"body.$.id.#", "", KindString, false},
		{"body.$.items[0].done.length", "", KindString, false},
		{"body.$.meta.nothing.#", "", KindString, false},
		// An object with a real "length" key keeps it.
		{"body.$.length", "99", KindNumber, true},
		{"body.$.nope.length", "", KindString, false},
		// The whole body.
		{"body.$[0]", "", KindString, false},
	}
	for _, c := range cases {
		v, ok, err := SelectValue(resp, c.sel)
		if err != nil || ok != c.ok || (ok && (v.Text != c.want || v.Kind != c.kind)) {
			t.Errorf("%s: got %q %v ok=%v err=%v; want %q %v ok=%v", c.sel, v.Text, v.Kind, ok, err, c.want, c.kind, c.ok)
		}
		if err := Check(c.sel); err != nil {
			t.Errorf("Check(%s) = %v", c.sel, err)
		}
	}
	v, ok, err := SelectValue(resp, "body.$")
	if err != nil || !ok || v.Kind != KindObject || !strings.HasPrefix(v.Text, "{") {
		t.Errorf("body.$ = %v %v %v", v, ok, err)
	}
}

// What cannot be read says what it was.
func TestPathErrors(t *testing.T) {
	cases := map[string]string{
		"body.$.items[0,1]":            "unions are not supported",
		"body.$.items[0:4:2]":          "slice steps are not supported",
		"body.$.items[x]":              "expected an index",
		"body.$.items[0":               "no closing ]",
		"body.$.items.":                "cannot end with a dot",
		"body.$.a..":                   "cannot end with a dot",
		"body.$..#":                    ".. must be followed by",
		"body.$.items[?(@.a ~= 1)]":    "unsupported filter",
		"body.$.items[?(@.a == nope)]": "expected a number, a quoted string",
		"body.$.items[?((@.a == 1))]":  "parentheses",
		"body.$.items[?(@.a =~ abc)]":  "needs a /regex/",
		"body.$.items[?(@.a =~ /x/g)]": "the only regex flag is i",
		"body.$.items[?(@.a =~ /(/)]":  "bad regex",
		"body.$.items[?(a == 1)]":      "must start with @",
		"body.$.items[?(!@.a == 1)]":   "only negates a presence test",
		"body.$.items[?()]":            "empty",
		"body.$.items[\"a]":            "no closing ]",
		"body.$.items[?(@.a[0 == 1)]":  "no closing ]",
		`body.$.items.#(id=="a").name`: "gjson queries are not supported",
		"header.x[1":                   "",
		"header.":                      "needs a name",
		"cookie.":                      "needs a name",
		"bogus":                        "unknown selector",
	}
	resp := &Response{Body: []byte(doc)}
	for sel, want := range cases {
		err := Check(sel)
		_, _, serr := SelectValue(resp, sel)
		if want == "" {
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Check(%s) = %v; want %q", sel, err, want)
		}
		if serr == nil || serr.Error() != err.Error() {
			t.Errorf("Select(%s) = %v; want the same error as Check", sel, serr)
		}
	}
}

func TestMultiValueHeaders(t *testing.T) {
	h := http.Header{}
	for _, c := range []string{"a=1", "b=2", "c=3"} {
		h.Add("Set-Cookie", c)
	}
	h.Add("Link", "<https://x/2>; rel=next")
	h.Add("X.Correlation-ID", "abc") // a dot is a token character
	resp := &Response{Headers: h}
	cases := []struct {
		sel, want string
		ok        bool
	}{
		{"header.set-cookie", "a=1", true},
		{"header.set-cookie.#", "3", true},
		{"header.set-cookie[1]", "b=2", true},
		{"header.set-cookie[-1]", "c=3", true},
		{"header.set-cookie[3]", "", false},
		{"header.link.#", "1", true},
		{"header.missing.#", "0", true},
		{"header.missing[0]", "", false},
		{"header.X.Correlation-Id", "abc", true},
		{"header.x.correlation-id.#", "1", true},
	}
	for _, c := range cases {
		got, ok, err := Select(resp, c.sel)
		if err != nil || ok != c.ok || got != c.want {
			t.Errorf("%s: %q %v %v; want %q %v", c.sel, got, ok, err, c.want, c.ok)
		}
	}
}

// The selector of an assertion runs to the first space outside a bracket,
// however deep, so a filter keeps its spaces and nested brackets.
func TestLeading(t *testing.T) {
	cases := []struct{ in, sel, rest string }{
		{`body.$.id == 2`, "body.$.id", "== 2"},
		{`body.$.items[?(@.tags[0] == "a b")].id == 1`, `body.$.items[?(@.tags[0] == "a b")].id`, "== 1"},
		{`body.$.items[?(@['x y'] == 1)] exists`, `body.$.items[?(@['x y'] == 1)]`, "exists"},
		{`body.$.items[?(@.n =~ /] x/)] exists`, `body.$.items[?(@.n =~ /] x/)]`, "exists"},
		{`body.$.it's == 1`, `body.$.it's`, "== 1"},
		{`body.$.items[0 == 1`, `body.$.items[0 == 1`, ""},
		{`  status   ==  200`, "status", "==  200"},
	}
	for _, c := range cases {
		sel, rest := Leading(c.in)
		if sel != c.sel || rest != c.rest {
			t.Errorf("Leading(%q) = %q, %q; want %q, %q", c.in, sel, rest, c.sel, c.rest)
		}
	}
}

// A single index or slice stops walking the array where it can.
func BenchmarkIndex(b *testing.B) {
	var sb strings.Builder
	sb.WriteString(`{"items":[`)
	for i := range 100000 {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"id":1}`)
	}
	sb.WriteString(`]}`)
	resp := &Response{Body: []byte(sb.String())}
	for b.Loop() {
		if _, ok, _ := Select(resp, "body.$.items[0].id"); !ok {
			b.Fatal("nothing at items[0].id")
		}
	}
}
