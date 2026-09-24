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

// Codes are unique, grouped by their first digit, and every entry has what
// docs/errors.md and the --json error object need.
func TestCatalogueIsWellFormed(t *testing.T) {
	seen := map[Code]bool{}
	for _, e := range Catalogue {
		if seen[e.Code] {
			t.Errorf("%s listed twice", e.Code)
		}
		seen[e.Code] = true
		if len(e.Code) != 4 || e.Code[0] != 'E' {
			t.Errorf("%s: codes look like E101", e.Code)
		}
		wantExit := ExitUsage
		if e.Code[1] == '3' {
			wantExit = ExitTransport
		}
		if e.Exit != wantExit {
			t.Errorf("%s: exit %d, want %d", e.Code, e.Exit, wantExit)
		}
		if e.Title == "" || e.Hint == "" || e.About == "" {
			t.Errorf("%s: needs a title, a hint and an explanation", e.Code)
		}
	}
}

// The exit status a code documents is the one ExitCode gives.
func TestInfoMatchesExitCode(t *testing.T) {
	for _, err := range []error{
		Usage(CodeFlag, "x"),
		&TransportError{Err: errors.New("boom")},
		fmt.Errorf("wrapped: %w", &TransportError{Code: CodeTimeout, Err: errors.New("slow")}),
		errors.New("plain"),
	} {
		if info := Info(err); info.Exit != ExitCode(err) {
			t.Errorf("%v: Info says exit %d, ExitCode %d", err, info.Exit, ExitCode(err))
		}
	}
	if got := CodeOf(fmt.Errorf("outer: %w", Usagef(CodeFlag, "inner: %v", Usage(CodeMissingVariable, "token")))); got != CodeMissingVariable {
		t.Errorf("an inner code should win, got %s", got)
	}
}
