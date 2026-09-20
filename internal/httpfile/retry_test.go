package httpfile

import (
	"testing"
	"time"
)

func TestParseRetry(t *testing.T) {
	good := map[string]struct {
		n int
		d time.Duration
	}{
		"1":        {1, time.Second},
		"10":       {10, time.Second},
		"5 500ms":  {5, 500 * time.Millisecond},
		" 30  2s ": {30, 2 * time.Second},
		"3 0s":     {3, 0},
		"2 1m30s":  {2, 90 * time.Second},
		"4\t250ms": {4, 250 * time.Millisecond},
	}
	for in, want := range good {
		n, d, err := ParseRetry(in)
		if err != nil || n != want.n || d != want.d {
			t.Errorf("ParseRetry(%q) = %d, %s, %v; want %d, %s", in, n, d, err, want.n, want.d)
		}
	}
	for _, in := range []string{"", "0", "-1", "x", "1.5", "3 soon", "3 -1s", "3 2s extra"} {
		if _, _, err := ParseRetry(in); err == nil {
			t.Errorf("ParseRetry(%q) should fail", in)
		}
	}
}
