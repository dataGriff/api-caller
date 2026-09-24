package runner

import (
	"errors"
	"fmt"
	"io"
)

// DefaultMaxBodyBytes caps how much of a response apic reads into memory when
// apic.yaml does not say otherwise.
//
// apic buffers a whole response before it can select, assert or render it, and
// holds it more than once while doing so, so an unbounded read turns one
// oversized or hostile response into a dead process. 64 MiB is far above any
// response a human reads in a terminal and still leaves the tool honest about
// what it cannot handle.
const DefaultMaxBodyBytes int64 = 64 << 20

func (r *Runner) maxBodyBytes() int64 {
	if r.Opts.MaxBodyBytes > 0 {
		return r.Opts.MaxBodyBytes
	}
	if r.Project != nil && r.Project.Config.MaxBodyBytes > 0 {
		return r.Project.Config.MaxBodyBytes
	}
	return DefaultMaxBodyBytes
}

// readBody reads at most max bytes, and reports an error rather than silently
// truncating: a half-read body would produce assertions and captures that
// quietly disagree with what the server actually sent.
// errBodyTooLarge marks a response larger than apic reads.
var errBodyTooLarge = errors.New("response body too large")

func readBody(rc io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%w: it exceeds %d bytes; raise maxBodyBytes in apic.yaml to read it", errBodyTooLarge, max)
	}
	return data, nil
}
