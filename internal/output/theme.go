package output

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds every style apic uses for human output, shared by the CLI
// renderers and the terminal UI so both look the same. Colours collapse to
// plain text automatically when lipgloss's colour profile is Ascii
// (--no-color, NO_COLOR, --json, or output that is not a terminal).
type Theme struct {
	Bold    lipgloss.Style
	Dim     lipgloss.Style
	OK      lipgloss.Style
	Warn    lipgloss.Style
	Fail    lipgloss.Style
	URL     lipgloss.Style
	Header  lipgloss.Style
	Capture lipgloss.Style
	Accent  lipgloss.Style

	// JSON syntax highlighting.
	Key lipgloss.Style
	Str lipgloss.Style
	Num lipgloss.Style
	Lit lipgloss.Style

	methods map[string]lipgloss.Style
	other   lipgloss.Style
}

// Default is the theme apic ships with.
func Default() Theme {
	c := func(n string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(n)) }
	return Theme{
		Bold:    lipgloss.NewStyle().Bold(true),
		Dim:     lipgloss.NewStyle().Faint(true),
		OK:      c("42").Bold(true),
		Warn:    c("214").Bold(true),
		Fail:    c("203").Bold(true),
		URL:     c("39"),
		Header:  c("141"),
		Capture: c("81"),
		Accent:  c("212").Bold(true),
		Key:     c("81"),
		Str:     c("114"),
		Num:     c("221"),
		Lit:     c("176"),
		methods: map[string]lipgloss.Style{
			"GET":     c("39").Bold(true),
			"HEAD":    c("39").Bold(true),
			"OPTIONS": c("39").Bold(true),
			"POST":    c("42").Bold(true),
			"PUT":     c("214").Bold(true),
			"PATCH":   c("214").Bold(true),
			"DELETE":  c("203").Bold(true),
		},
		other: c("176").Bold(true),
	}
}

// Method renders an HTTP method in its colour.
func (t Theme) Method(m string) string {
	if s, ok := t.methods[m]; ok {
		return s.Render(m)
	}
	return t.other.Render(m)
}

// statusStyle picks the style for a status class.
func (t Theme) statusStyle(code int) lipgloss.Style {
	switch {
	case code < 300:
		return t.OK
	case code < 400:
		return t.Warn
	default:
		return t.Fail
	}
}

// Status renders "200 OK" coloured by status class.
func (t Theme) Status(code int, text string) string {
	return t.statusStyle(code).Render(fmt.Sprintf("%d %s", code, text))
}

// StatusCode renders the status number on its own, coloured by class, for
// the places too narrow for the reason phrase.
func (t Theme) StatusCode(code int) string {
	return t.statusStyle(code).Render(strconv.Itoa(code))
}

// Latency renders a duration in milliseconds, coloured by how long it took.
func (t Theme) Latency(ms int64) string {
	s := fmt.Sprintf("%d ms", ms)
	switch {
	case ms < 200:
		return t.OK.Render(s)
	case ms < 1000:
		return t.Warn.Render(s)
	default:
		return t.Fail.Render(s)
	}
}

// Mark renders a pass or fail tick.
func (t Theme) Mark(pass bool) string {
	if pass {
		return t.OK.Render("✓")
	}
	return t.Fail.Render("✗")
}
