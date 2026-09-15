package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// This file turns a terminal frame — lines of text carrying SGR escapes —
// into a standalone SVG drawn inside a window, so a screenshot in the docs
// is the bytes apic really writes to a terminal rather than a drawing of
// them.

// Geometry of the window, in SVG user units. The character advance is the
// one DejaVu Sans Mono uses at this size, and every character is placed on
// it explicitly, so the grid holds whichever monospace font the reader has.
const (
	fontSize     = 13.0
	cellW        = fontSize * 0.60129
	lineH        = 18.0
	padX         = 18.0
	barH         = 38.0
	firstBase    = 60.0 // baseline of the first line of text
	bottomMargin = 14.0
)

// Window colours, matching the docs theme.
const (
	windowBG     = "#11131a"
	barBG        = "#1b1e28"
	titleFG      = "#8b93a7"
	defaultFG    = "#d6dae4"
	faintOpacity = "0.55"
)

// style is the SGR state in force for a run of characters.
type style struct {
	fg, bg    string // "" means the default
	bold      bool
	faint     bool
	underline bool
}

// run is a stretch of characters sharing one style.
type run struct {
	col   int // starting column
	width int // columns it occupies
	text  string
	style style
}

// SVG renders frame — a whole terminal screen, lines separated by \n — as an
// SVG window titled with the command that produced it.
func SVG(title, frame string) string {
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	cols := 0
	parsed := make([][]run, len(lines))
	for i, line := range lines {
		parsed[i] = split(line)
		// The full width, trailing spaces and all: a UI frame pads every
		// line to the terminal width, and the window has to be that wide
		// even though blank runs are never drawn.
		if w := ansi.StringWidth(ansi.Strip(line)); w > cols {
			cols = w
		}
	}
	if cols < ansi.StringWidth(title)+8 {
		cols = ansi.StringWidth(title) + 8
	}
	width := roundTo(2*padX + float64(cols)*cellW)
	height := roundTo(firstBase + float64(len(lines)-1)*lineH + bottomMargin)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" `+
		`font-family="ui-monospace,SFMono-Regular,Menlo,Consolas,monospace" font-size="%g">`+"\n",
		width, height, width, height, fontSize)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" rx="10" fill="%s"/>`+"\n", width, height, windowBG)
	fmt.Fprintf(&b, `<rect width="%d" height="%g" rx="10" fill="%s"/>`+"\n", width, barH, barBG)
	fmt.Fprintf(&b, `<rect y="%g" width="%d" height="10" fill="%s"/>`+"\n", barH-10, width, barBG)
	for i, c := range []string{"#ff5f57", "#febc2e", "#28c840"} {
		fmt.Fprintf(&b, `<circle cx="%d" cy="19" r="5.5" fill="%s"/>`+"\n", 18+i*17, c)
	}
	fmt.Fprintf(&b, `<text x="%g" y="23.5" fill="%s" text-anchor="middle" font-size="12">%s</text>`+"\n",
		float64(width)/2, titleFG, escape(title))

	for i, runs := range parsed {
		y := firstBase + float64(i)*lineH
		for _, r := range runs {
			if s := r.render(y); s != "" {
				b.WriteString(s + "\n")
			}
		}
	}
	b.WriteString("</svg>\n")
	return b.String()
}

// render draws one run: its background, if it has one, then the characters
// at their exact column positions.
func (r run) render(y float64) string {
	blank := strings.TrimSpace(r.text) == ""
	if blank && r.style.bg == "" {
		return "" // nothing to draw, and the window background shows through
	}
	var b strings.Builder
	if r.style.bg != "" {
		fmt.Fprintf(&b, `<rect x="%s" y="%s" width="%s" height="%g" fill="%s"/>`,
			num(padX+float64(r.col)*cellW), num(y-fontSize), num(float64(r.width)*cellW), lineH, r.style.bg)
		if blank {
			return b.String()
		}
		b.WriteString("\n")
	}
	xs := make([]string, 0, len(r.text))
	col := r.col
	for _, c := range r.text {
		xs = append(xs, num(padX+float64(col)*cellW))
		col += ansi.StringWidth(string(c))
	}
	fill := r.style.fg
	if fill == "" {
		fill = defaultFG
	}
	fmt.Fprintf(&b, `<text x="%s" y="%s" fill="%s"`, strings.Join(xs, " "), num(y), fill)
	if r.style.bold {
		b.WriteString(` font-weight="bold"`)
	}
	if r.style.faint {
		fmt.Fprintf(&b, ` opacity="%s"`, faintOpacity)
	}
	if r.style.underline {
		b.WriteString(` text-decoration="underline"`)
	}
	fmt.Fprintf(&b, ` xml:space="preserve">%s</text>`, escape(r.text))
	return b.String()
}

// split breaks one line into runs of equal style, applying every SGR escape
// it meets and skipping any other escape sequence.
func split(line string) []run {
	var (
		out  []run
		cur  = run{}
		open bool
		st   style
		col  int
	)
	flush := func() {
		if open && cur.text != "" {
			out = append(out, cur)
		}
		open = false
	}
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			n, params, isSGR := escapeAt(line[i:])
			if n == 0 {
				i++ // a stray escape byte
				continue
			}
			if isSGR {
				next := apply(st, params)
				if next != st {
					flush()
					st = next
				}
			}
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		i += size
		w := ansi.StringWidth(string(r))
		if !open {
			cur, open = run{col: col, style: st}, true
		}
		cur.text += string(r)
		cur.width += w
		col += w
	}
	flush()
	return out
}

// escapeAt measures the escape sequence at the start of s, and returns its
// parameters when it is an SGR ("...m") sequence.
func escapeAt(s string) (n int, params []string, isSGR bool) {
	if len(s) < 2 || s[0] != 0x1b {
		return 0, nil, false
	}
	if s[1] != '[' {
		return 2, nil, false // a two-byte escape the frame never uses
	}
	for i := 2; i < len(s); i++ {
		if s[i] >= 0x40 && s[i] <= 0x7e {
			if s[i] != 'm' {
				return i + 1, nil, false
			}
			return i + 1, strings.Split(s[2:i], ";"), true
		}
	}
	return len(s), nil, false
}

// apply folds one SGR sequence into the style in force.
func apply(st style, params []string) style {
	if len(params) == 0 || (len(params) == 1 && params[0] == "") {
		return style{}
	}
	for i := 0; i < len(params); i++ {
		p, err := strconv.Atoi(params[i])
		if err != nil {
			continue
		}
		switch {
		case p == 0:
			st = style{}
		case p == 1:
			st.bold = true
		case p == 2:
			st.faint = true
		case p == 4:
			st.underline = true
		case p == 22:
			st.bold, st.faint = false, false
		case p == 24:
			st.underline = false
		case p == 39:
			st.fg = ""
		case p == 49:
			st.bg = ""
		case p >= 30 && p <= 37:
			st.fg = palette(p - 30)
		case p >= 90 && p <= 97:
			st.fg = palette(p - 90 + 8)
		case p >= 40 && p <= 47:
			st.bg = palette(p - 40)
		case p >= 100 && p <= 107:
			st.bg = palette(p - 100 + 8)
		case p == 38 || p == 48:
			c, used := extended(params[i+1:])
			if c != "" {
				if p == 38 {
					st.fg = c
				} else {
					st.bg = c
				}
			}
			i += used
		}
	}
	return st
}

// extended reads the tail of a 38/48 sequence: ";5;n" for a palette index
// and ";2;r;g;b" for a direct colour.
func extended(rest []string) (colour string, used int) {
	if len(rest) == 0 {
		return "", 0
	}
	switch rest[0] {
	case "5":
		if len(rest) < 2 {
			return "", len(rest)
		}
		n, err := strconv.Atoi(rest[1])
		if err != nil {
			return "", 2
		}
		return palette(n), 2
	case "2":
		if len(rest) < 4 {
			return "", len(rest)
		}
		var rgb [3]int
		for i := range rgb {
			v, err := strconv.Atoi(rest[i+1])
			if err != nil {
				return "", 4
			}
			rgb[i] = clamp(v)
		}
		return fmt.Sprintf("#%02x%02x%02x", rgb[0], rgb[1], rgb[2]), 4
	}
	return "", 1
}

// base16 is the low end of the palette: the colours a terminal theme picks
// for itself. These match the window background above.
var base16 = [16]string{
	"#2a2e3a", "#ff5f57", "#28c840", "#febc2e", "#5aa9f8", "#d67bff", "#4de0e0", "#d6dae4",
	"#555b6b", "#ff8a84", "#5ff08a", "#ffd479", "#8ec6ff", "#e6a6ff", "#8ff0f0", "#ffffff",
}

// palette maps an xterm-256 index to a hex colour: sixteen themed colours,
// then the 6×6×6 cube, then the greyscale ramp.
func palette(n int) string {
	switch {
	case n < 0 || n > 255:
		return defaultFG
	case n < 16:
		return base16[n]
	case n < 232:
		levels := [6]int{0, 95, 135, 175, 215, 255}
		n -= 16
		return fmt.Sprintf("#%02x%02x%02x", levels[n/36], levels[(n/6)%6], levels[n%6])
	default:
		v := 8 + (n-232)*10
		return fmt.Sprintf("#%02x%02x%02x", v, v, v)
	}
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// num formats a coordinate the way the rest of the file does: one decimal,
// with a trailing ".0" kept so columns line up in a diff.
func num(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

func roundTo(f float64) int { return int(f + 0.5) }

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func escape(s string) string { return escaper.Replace(s) }
