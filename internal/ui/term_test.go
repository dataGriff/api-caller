package ui

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
		used int
	}{
		{"j", "j", 1},
		{"\r", "enter", 1},
		{"\n", "enter", 1},
		{"\t", "tab", 1},
		{"\x7f", "backspace", 1},
		{"\x03", "ctrl+c", 1},
		{"\x15", "ctrl+u", 1},
		{"\x1b[A", "up", 3},
		{"\x1b[B", "down", 3},
		{"\x1bOC", "right", 3},
		{"\x1b[Z", "shift+tab", 3},
		{"\x1b[5~", "pgup", 4},
		{"\x1b[6~", "pgdown", 4},
		{"\x1b[H", "home", 3},
		{"é", "é", 2},
	}
	for _, c := range cases {
		k, used, ok := decodeKey([]byte(c.in))
		if !ok || k.String() != c.want || used != c.used {
			t.Errorf("decodeKey(%q) = %q,%d,%v; want %q,%d", c.in, k.String(), used, ok, c.want, c.used)
		}
	}
	// An unknown CSI sequence (a mouse report) is consumed, not shown.
	if k, used, ok := decodeKey([]byte("\x1b[<0;1;1M")); ok || used != 9 {
		t.Errorf("mouse report: %q,%d,%v", k.String(), used, ok)
	}
	// Incomplete sequences ask for more bytes; readKeys resolves a lone ESC.
	for _, in := range []string{"\x1b", "\x1b[", "\x1b[5"} {
		if _, used, ok := decodeKey([]byte(in)); ok || used != 0 {
			t.Errorf("decodeKey(%q) should consume nothing, got %d,%v", in, used, ok)
		}
	}
}

func TestReadKeysSplitsAcrossReads(t *testing.T) {
	pr, pw := io.Pipe()
	keys := make(chan Key, 8)
	done := make(chan struct{})
	defer close(done)
	go readKeys(pr, keys, done)
	go func() {
		_, _ = pw.Write([]byte("ab\x1b["))
		time.Sleep(10 * time.Millisecond)
		_, _ = pw.Write([]byte("A\r"))
		_ = pw.Close()
	}()
	var got []string
	for i := 0; i < 4; i++ {
		select {
		case k := <-keys:
			got = append(got, k.String())
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after %v", got)
		}
	}
	if strings.Join(got, ",") != "a,b,up,enter" {
		t.Fatalf("got %v", got)
	}
}

func TestReadKeysResolvesLoneEscape(t *testing.T) {
	pr, pw := io.Pipe()
	keys := make(chan Key, 4)
	done := make(chan struct{})
	defer close(done)
	go readKeys(pr, keys, done)
	if _, err := pw.Write([]byte{0x1b}); err != nil {
		t.Fatal(err)
	}
	select {
	case k := <-keys:
		if k.String() != "esc" {
			t.Fatalf("got %q", k.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a lone escape should arrive after the grace period")
	}
	_ = pw.Close()
}

func TestViewport(t *testing.T) {
	v := viewport{width: 10, height: 3}
	v.setContent("1\n2\n3\n4\n5\n6")
	if v.view() != "1\n2\n3" {
		t.Fatalf("top: %q", v.view())
	}
	v.halfPageDown()
	if v.view() != "2\n3\n4" {
		t.Fatalf("after half page: %q", v.view())
	}
	v.scroll(50)
	if v.view() != "4\n5\n6" || !v.atBottom() {
		t.Fatalf("clamped to the end: %q", v.view())
	}
	v.gotoTop()
	if v.view() != "1\n2\n3" {
		t.Fatalf("gotoTop: %q", v.view())
	}
	// Short content is padded to the window height.
	v.setContent("only")
	if v.view() != "only\n\n" {
		t.Fatalf("padding: %q", v.view())
	}
}

func TestSpinnerCycles(t *testing.T) {
	s := newSpinner()
	first := s.view()
	for i := 0; i < len(s.frames); i++ {
		s.tick()
	}
	if s.view() != first {
		t.Fatalf("spinner should wrap around: %q != %q", s.view(), first)
	}
}
