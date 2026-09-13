package bdd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/dataGriff/api-caller/internal/assert"
)

// decodeJSON keeps numbers as json.Number so large integers compare exactly.
func decodeJSON(data []byte, v *any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	// Exactly one value: a second value or trailing syntax is an error.
	var rest any
	if err := dec.Decode(&rest); err != io.EOF {
		return fmt.Errorf("unexpected data after the JSON value")
	}
	return nil
}

// jsonEqual reports whether two JSON documents are semantically equal.
func jsonEqual(actual, expected []byte) (bool, string) {
	var a, e any
	if err := decodeJSON(actual, &a); err != nil {
		return false, "response body is not JSON"
	}
	if err := decodeJSON(expected, &e); err != nil {
		return false, "expected document is not JSON: " + err.Error()
	}
	if why := diff("$", a, e, true); why != "" {
		return false, why
	}
	return true, ""
}

// jsonContains reports whether expected is a subset of actual: every key in
// an expected object must be present and match; arrays must have the same
// length and match element by element; scalars must be equal.
func jsonContains(actual, expected []byte) (bool, string) {
	var a, e any
	if err := decodeJSON(actual, &a); err != nil {
		return false, "response body is not JSON"
	}
	if err := decodeJSON(expected, &e); err != nil {
		return false, "expected document is not JSON: " + err.Error()
	}
	if why := diff("$", a, e, false); why != "" {
		return false, why
	}
	return true, ""
}

func diff(path string, a, e any, exact bool) string {
	switch ev := e.(type) {
	case map[string]any:
		av, ok := a.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: expected an object, got %s", path, kind(a))
		}
		keys := make([]string, 0, len(ev))
		for k := range ev {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			got, present := av[k]
			if !present {
				return fmt.Sprintf("%s.%s: missing", path, k)
			}
			if why := diff(path+"."+k, got, ev[k], exact); why != "" {
				return why
			}
		}
		if exact && len(av) != len(ev) {
			for k := range av {
				if _, ok := ev[k]; !ok {
					return fmt.Sprintf("%s.%s: unexpected key", path, k)
				}
			}
		}
	case []any:
		av, ok := a.([]any)
		if !ok {
			return fmt.Sprintf("%s: expected an array, got %s", path, kind(a))
		}
		if len(av) != len(ev) {
			return fmt.Sprintf("%s: expected %d items, got %d", path, len(ev), len(av))
		}
		for i := range ev {
			if why := diff(fmt.Sprintf("%s[%d]", path, i), av[i], ev[i], exact); why != "" {
				return why
			}
		}
	case json.Number:
		an, ok := a.(json.Number)
		if !ok || !numberEqual(an, ev) {
			return fmt.Sprintf("%s: expected %s, got %s", path, show(e), show(a))
		}
	default:
		if !reflect.DeepEqual(a, e) {
			return fmt.Sprintf("%s: expected %s, got %s", path, show(e), show(a))
		}
	}
	return ""
}

// numberEqual compares JSON numbers exactly, so 1.0 equals 1 and
// 9007199254740993 differs from 9007199254740992. Numbers too large to
// expand safely (see assert.ParseNumber) compare as text.
func numberEqual(a, b json.Number) bool {
	x, ok1 := assert.ParseNumber(a.String())
	y, ok2 := assert.ParseNumber(b.String())
	if !ok1 || !ok2 {
		return a == b
	}
	return x.Cmp(y) == 0
}

func kind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case json.Number:
		return "a number"
	case bool:
		return "a boolean"
	}
	return "unknown"
}

func show(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return strings.TrimSpace(s)
}
