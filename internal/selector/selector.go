// Package selector extracts values from an HTTP response using the small
// selector language shared by `# @capture` and `# @assert`:
//
//	status                 response status code
//	statusText             e.g. "OK"
//	header.<name>          first value of a response header (case-insensitive)
//	body                   raw body
//	body.$                 whole body as JSON
//	body.$.<path>          JSONPath-like subset, e.g. body.$.items[0].id
//	duration               round-trip time in milliseconds
package selector

import (
	"fmt"
	"net/http"
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

// Select evaluates sel against resp. ok=false means the selector was valid
// but nothing was there (missing header or JSON path).
func Select(resp *Response, sel string) (value string, ok bool, err error) {
	sel = strings.TrimSpace(sel)
	switch {
	case sel == "status":
		return strconv.Itoa(resp.Status), true, nil
	case sel == "statusText":
		return resp.StatusText, true, nil
	case sel == "duration":
		return strconv.FormatInt(resp.Duration.Milliseconds(), 10), true, nil
	case sel == "body":
		return string(resp.Body), true, nil
	case strings.HasPrefix(sel, "header.") || strings.HasPrefix(sel, "headers."):
		name := sel[strings.Index(sel, ".")+1:]
		if name == "" {
			return "", false, fmt.Errorf("header selector needs a name")
		}
		v := resp.Headers.Values(name)
		if len(v) == 0 {
			return "", false, nil
		}
		return v[0], true, nil
	case sel == "body.$":
		if !gjson.ValidBytes(resp.Body) {
			return "", false, fmt.Errorf("body is not valid JSON")
		}
		return string(resp.Body), true, nil
	case strings.HasPrefix(sel, "body.$."), strings.HasPrefix(sel, "body.$["):
		if !gjson.ValidBytes(resp.Body) {
			return "", false, fmt.Errorf("body is not valid JSON")
		}
		path := ToGJSON(sel[len("body.$"):])
		r := gjson.GetBytes(resp.Body, path)
		if !r.Exists() {
			return "", false, nil
		}
		if r.Type == gjson.String {
			return r.String(), true, nil
		}
		return r.Raw, true, nil
	}
	return "", false, fmt.Errorf("unknown selector %q (expected status, header.<name>, body, body.$.<path> or duration)", sel)
}

// ToGJSON converts a JSONPath-like tail such as `.items[0].id` or
// `["key with space"]` into a gjson path (`items.0.id`).
func ToGJSON(tail string) string {
	var b strings.Builder
	i := 0
	for i < len(tail) {
		switch c := tail[i]; c {
		case '.':
			i++
			if b.Len() > 0 {
				b.WriteByte('.')
			}
		case '[':
			end := strings.IndexByte(tail[i:], ']')
			if end < 0 {
				b.WriteString(tail[i:])
				return b.String()
			}
			inner := strings.Trim(tail[i+1:i+end], `"'`)
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(escapeSeg(inner))
			i += end + 1
		default:
			end := strings.IndexAny(tail[i:], ".[")
			if end < 0 {
				end = len(tail) - i
			}
			b.WriteString(escapeSeg(tail[i : i+end]))
			i += end
		}
	}
	return b.String()
}

func escapeSeg(s string) string {
	if s == "*" || s == "#" {
		return s
	}
	r := strings.NewReplacer(".", `\.`, "*", `\*`, "?", `\?`)
	return r.Replace(s)
}
