package httpfile

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultRetryInterval is the wait between attempts when a retry policy
// gives none.
const DefaultRetryInterval = time.Second

// ParseRetry parses a retry policy, "<attempts> [interval]": how many times
// to send a request until its assertions pass, and how long to wait between
// attempts (a Go duration such as 500ms or 2s; default 1s). Attempts must be
// at least 1. It is the value of `# @retry`, of `--retry` and of retry in
// apic.yaml.
func ParseRetry(s string) (n int, interval time.Duration, err error) {
	fields := strings.Fields(s)
	if len(fields) == 0 || len(fields) > 2 {
		return 0, 0, errors.New("want `<attempts> [interval]`, for example `10 2s`")
	}
	n, err = strconv.Atoi(fields[0])
	if err != nil || n < 1 {
		return 0, 0, fmt.Errorf("attempts must be a whole number of at least 1, got %q", fields[0])
	}
	interval = DefaultRetryInterval
	if len(fields) == 2 {
		interval, err = time.ParseDuration(fields[1])
		if err != nil || interval < 0 {
			return 0, 0, fmt.Errorf("interval must be a duration such as 500ms or 2s, got %q", fields[1])
		}
	}
	return n, interval, nil
}
