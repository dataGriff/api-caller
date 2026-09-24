package httpfile

import (
	"fmt"
	"strings"
	"time"
)

// Disabled reports `# @disabled`: the request is skipped when its file runs
// as a flow, and still sent when it is asked for by name.
func (r *Request) Disabled() bool {
	_, ok := r.Directive("disabled")
	return ok
}

// Sleep is how long `# @sleep <duration>` waits before the request is
// sent; zero without the directive.
func (r *Request) Sleep() (time.Duration, error) {
	v, ok := r.Directive("sleep")
	if !ok {
		return 0, nil
	}
	return ParseSleep(v)
}

// ParseSleep reads the value of `# @sleep`: a Go duration such as 500ms or
// 2s, not negative.
func ParseSleep(s string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || d < 0 {
		return 0, fmt.Errorf("want a duration such as 500ms or 2s, got %q", s)
	}
	return d, nil
}

// The HTTP versions a request line can ask for.
const (
	HTTP1 = "HTTP/1.1"
	HTTP2 = "HTTP/2"
)

// Protocol is the HTTP version the request line asks for: "" when it names
// none, HTTP1 (HTTP/1.0 is sent as 1.1) or HTTP2. HTTP/3 and anything else
// is an error, which validate reports as bad-http-version.
func (r *Request) Protocol() (string, error) {
	switch strings.ToUpper(strings.TrimSpace(r.HTTPVersion)) {
	case "":
		return "", nil
	case "HTTP/1.1", "HTTP/1.0", "HTTP/1":
		return HTTP1, nil
	case "HTTP/2", "HTTP/2.0":
		return HTTP2, nil
	}
	return "", fmt.Errorf("HTTP version %q is not supported: write HTTP/1.1 or HTTP/2, or leave it out", r.HTTPVersion)
}
