package runner

import (
	"errors"
	"fmt"
)

// Exit codes returned by the CLI.
const (
	ExitOK        = 0
	ExitAssert    = 1 // an assertion or capture failed
	ExitUsage     = 2 // parse error, unknown request, missing variable
	ExitTransport = 3 // could not reach the server, timeout, TLS failure
)

// UsageError is a problem with the request definition or invocation.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// TransportError is a network-level failure.
type TransportError struct{ Err error }

func (e *TransportError) Error() string { return "request failed: " + e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

func usagef(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// ExitCode maps an error to the CLI exit code.
//
// The exit codes are a public contract, so this unwraps rather than switching
// on the concrete type: wrapping a TransportError with %w anywhere on the way
// up would otherwise turn a documented 3 into a 2.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var te *TransportError
	if errors.As(err, &te) {
		return ExitTransport
	}
	return ExitUsage
}
