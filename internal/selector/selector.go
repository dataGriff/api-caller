// Package selector extracts values from an HTTP response using the small
// selector language shared by `# @capture` and `# @assert`:
//
//	status                 response status code
//	statusText             e.g. "OK"
//	header.<name>          first value of a response header (case-insensitive)
//	header.<name>.#        how many values it has; header.<name>[1] the second
//	cookie.<name>          value of a cookie the response set (Set-Cookie)
//	body                   raw body
//	body.$                 whole body as JSON
//	body.$.<path>          JSONPath: body.$.items[0].id, body.$..id,
//	                       body.$.items[?(@.done == true)].id, [-1], [1:3],
//	                       .# and .length; see path.go
//	duration               round-trip time in milliseconds
package selector

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// Response is the subset of an HTTP response the selector language sees.
type Response struct {
	Status     int
	StatusText string
	Headers    http.Header
	Body       []byte
	Duration   time.Duration
}

// UnknownError is a selector that is none of the known forms (as opposed
// to a known form whose body path cannot be read).
type UnknownError struct{ Selector string }

func (e *UnknownError) Error() string {
	return fmt.Sprintf("unknown selector %q (expected status, statusText, header.<name>, cookie.<name>, body, body.$.<path> or duration)", e.Selector)
}

// Kind is the JSON type of a selected value. Status, duration and counts
// are numbers; headers, cookies, statusText and the raw body are strings;
// a body path has the type of what it selects, and a set of matches is
// an array.
type Kind int

const (
	KindString Kind = iota
	KindNumber
	KindBool
	KindNull
	KindArray
	KindObject
)

func (k Kind) String() string {
	return [...]string{"string", "number", "boolean", "null", "array", "object"}[k]
}

// Value is a selected value: its text as captures and assertions see it
// (a string unquoted, anything else as JSON), and its type.
type Value struct {
	Text string
	Kind Kind
}

// Select evaluates sel against resp. ok=false means the selector was valid
// but nothing was there (missing header or JSON path).
func Select(resp *Response, sel string) (value string, ok bool, err error) {
	v, ok, err := SelectValue(resp, sel)
	return v.Text, ok, err
}

// SelectValue is Select with the value's JSON type, for type checks.
func SelectValue(resp *Response, sel string) (Value, bool, error) {
	sel = strings.TrimSpace(sel)
	switch {
	case sel == "status":
		return Value{strconv.Itoa(resp.Status), KindNumber}, true, nil
	case sel == "statusText":
		return Value{resp.StatusText, KindString}, true, nil
	case sel == "duration":
		return Value{strconv.FormatInt(resp.Duration.Milliseconds(), 10), KindNumber}, true, nil
	case sel == "body":
		return Value{string(resp.Body), KindString}, true, nil
	case strings.HasPrefix(sel, "header.") || strings.HasPrefix(sel, "headers."):
		h, err := parseHeader(sel)
		if err != nil {
			return Value{}, false, err
		}
		v := resp.Headers.Values(h.name)
		switch {
		case h.count:
			return Value{strconv.Itoa(len(v)), KindNumber}, true, nil
		case h.index != nil:
			i := *h.index
			if i < 0 {
				i += len(v)
			}
			if i < 0 || i >= len(v) {
				return Value{}, false, nil
			}
			return Value{v[i], KindString}, true, nil
		case len(v) == 0:
			return Value{}, false, nil
		}
		return Value{v[0], KindString}, true, nil
	case strings.HasPrefix(sel, "cookie."):
		name := sel[len("cookie."):]
		if name == "" {
			return Value{}, false, fmt.Errorf("cookie selector needs a name")
		}
		for _, c := range (&http.Response{Header: resp.Headers}).Cookies() {
			if c.Name == name {
				return Value{c.Value, KindString}, true, nil
			}
		}
		return Value{}, false, nil
	case sel == "body.$" || strings.HasPrefix(sel, "body.$.") || strings.HasPrefix(sel, "body.$["):
		path, err := ParsePath(sel[len("body.$"):])
		if err != nil {
			return Value{}, false, err
		}
		if !gjson.ValidBytes(resp.Body) {
			return Value{}, false, fmt.Errorf("body is not valid JSON")
		}
		nodes, count, many := path.Eval(gjson.ParseBytes(resp.Body))
		switch {
		case count != nil:
			return Value{strconv.Itoa(*count), KindNumber}, true, nil
		case len(nodes) == 0:
			return Value{}, false, nil
		case many:
			raws := make([]string, len(nodes))
			for i, n := range nodes {
				raws[i] = n.Raw
			}
			return Value{"[" + strings.Join(raws, ",") + "]", KindArray}, true, nil
		}
		return valueOf(nodes[0]), true, nil
	}
	return Value{}, false, &UnknownError{Selector: sel}
}

// Check parses a selector without a response, so validate reports a bad
// one before anything is sent. The error names what could not be read.
func Check(sel string) error {
	sel = strings.TrimSpace(sel)
	switch {
	case sel == "status", sel == "statusText", sel == "duration", sel == "body":
		return nil
	case strings.HasPrefix(sel, "header.") || strings.HasPrefix(sel, "headers."):
		_, err := parseHeader(sel)
		return err
	case strings.HasPrefix(sel, "cookie."):
		if sel == "cookie." {
			return fmt.Errorf("cookie selector needs a name")
		}
		return nil
	case sel == "body.$" || strings.HasPrefix(sel, "body.$.") || strings.HasPrefix(sel, "body.$["):
		_, err := ParsePath(sel[len("body.$"):])
		return err
	}
	return &UnknownError{Selector: sel}
}

func valueOf(r gjson.Result) Value {
	switch r.Type {
	case gjson.String:
		return Value{r.Str, KindString}
	case gjson.Number:
		return Value{r.Raw, KindNumber}
	case gjson.True, gjson.False:
		return Value{r.Raw, KindBool}
	case gjson.Null:
		return Value{r.Raw, KindNull}
	}
	if r.IsArray() {
		return Value{r.Raw, KindArray}
	}
	return Value{r.Raw, KindObject}
}

type headerSel struct {
	name  string
	count bool
	index *int
}

var reHeaderIndex = regexp.MustCompile(`^(.+)\[(-?\d+)\]$`)

// parseHeader reads `header.<name>`, `header.<name>.#` (how many values)
// and `header.<name>[n]` (the n-th, negative from the end). A header name
// may itself contain dots, so only those two suffixes are special.
func parseHeader(sel string) (headerSel, error) {
	name := sel[strings.Index(sel, ".")+1:]
	var h headerSel
	switch {
	case strings.HasSuffix(name, ".#"):
		h.name, h.count = strings.TrimSuffix(name, ".#"), true
	case reHeaderIndex.MatchString(name):
		m := reHeaderIndex.FindStringSubmatch(name)
		n, _ := strconv.Atoi(m[2])
		h.name, h.index = m[1], &n
	default:
		h.name = name
	}
	if h.name == "" {
		return h, fmt.Errorf("header selector needs a name")
	}
	if strings.ContainsAny(h.name, "[]") {
		return h, fmt.Errorf("unsupported header selector %q: use header.<name>, header.<name>.# or header.<name>[n]", sel)
	}
	return h, nil
}
