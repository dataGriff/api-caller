package ui

import "strings"

// viewport is a scrollable window over pre-wrapped lines.
type viewport struct {
	lines         []string
	offset        int
	width, height int
}

func (v *viewport) setContent(s string) {
	v.lines = strings.Split(s, "\n")
	v.clamp()
}

func (v *viewport) clamp() {
	max := len(v.lines) - v.height
	if max < 0 {
		max = 0
	}
	if v.offset > max {
		v.offset = max
	}
	if v.offset < 0 {
		v.offset = 0
	}
}

func (v *viewport) gotoTop() { v.offset = 0 }

func (v *viewport) scroll(n int) {
	v.offset += n
	v.clamp()
}

func (v *viewport) halfPageUp()   { v.scroll(-v.height / 2) }
func (v *viewport) halfPageDown() { v.scroll(v.height / 2) }

// atBottom reports whether the last line is visible.
func (v *viewport) atBottom() bool { return v.offset >= len(v.lines)-v.height }

// view returns exactly height lines, padded with blanks.
func (v *viewport) view() string {
	if v.height <= 0 {
		return ""
	}
	v.clamp()
	end := v.offset + v.height
	if end > len(v.lines) {
		end = len(v.lines)
	}
	out := make([]string, 0, v.height)
	if v.offset < end {
		out = append(out, v.lines[v.offset:end]...)
	}
	for len(out) < v.height {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

// spinner is a small braille spinner advanced by the program's ticker.
type spinner struct {
	frames []string
	i      int
}

func newSpinner() spinner {
	return spinner{frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}}
}

func (s *spinner) tick() { s.i = (s.i + 1) % len(s.frames) }

func (s spinner) view() string { return s.frames[s.i] }
