package runner

import (
	"errors"
	"fmt"
	"testing"
)

// TestExitCodeUnwraps pins the documented exit codes against wrapping. A
// TransportError buried under fmt.Errorf("%w") must still exit 3, not 2.
func TestExitCodeUnwraps(t *testing.T) {
	transport := &TransportError{Err: errors.New("dial tcp: refused")}
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"transport", transport, ExitTransport},
		{"wrapped transport", fmt.Errorf("running login: %w", transport), ExitTransport},
		{"twice-wrapped transport", fmt.Errorf("flow: %w", fmt.Errorf("running login: %w", transport)), ExitTransport},
		{"usage", &UsageError{Msg: "unknown request"}, ExitUsage},
		{"wrapped usage", fmt.Errorf("resolving: %w", &UsageError{Msg: "missing variable"}), ExitUsage},
		{"plain", errors.New("something else"), ExitUsage},
	}
	for _, c := range cases {
		if got := ExitCode(c.err); got != c.want {
			t.Errorf("%s: ExitCode = %d, want %d", c.name, got, c.want)
		}
	}
}
