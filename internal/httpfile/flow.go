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
