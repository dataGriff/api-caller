package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/curlexport"
	"github.com/dataGriff/api-caller/internal/output"
)

// renderTabs draws the tab strip above the viewport.
func (m *Model) renderTabs(width int) string {
	t := m.theme
	var parts []string
	for i, name := range tabNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if tab(i) == m.tab {
			parts = append(parts, t.Accent.Render(label))
		} else {
			parts = append(parts, t.Dim.Render(label))
		}
	}
	line := strings.Join(parts, "")
	if m.selected != nil {
		line += "  " + t.Dim.Render(ansi.Truncate(m.selected.ID(), 30, "…"))
	}
	return ansi.Truncate(line, width, "…")
}

// renderPane returns the content of the current tab.
func (m *Model) renderPane(width int) string {
	if m.showHelp {
		return m.renderHelp()
	}
	if m.confirmClear {
		return m.theme.Warn.Render(fmt.Sprintf("clear the session for %q? (y/n)", m.envLabel()))
	}
	if m.selected == nil && m.tab != tabSession {
		return m.theme.Dim.Render("no request selected")
	}
	switch m.tab {
	case tabPreview:
		return m.renderPreview()
	case tabResponse:
		return m.renderResponse(width)
	case tabChecks:
		return m.renderChecks(width)
	default:
		return m.renderSession(width)
	}
}

func (m *Model) envLabel() string {
	if m.env == "" {
		return "default"
	}
	return m.env
}

func (m *Model) renderPreview() string {
	d := m.descs[m.selected]
	if d == nil {
		return m.theme.Dim.Render("…")
	}
	return output.Describe(m.theme, d, m.selected.Headers)
}

func (m *Model) renderResponse(width int) string {
	t := m.theme
	req := m.selected
	if err := m.errs[req]; err != nil {
		res := m.results[req]
		var b strings.Builder
		if res != nil {
			b.WriteString(output.RequestLine(t, res) + "\n\n")
		}
		b.WriteString(t.Fail.Render("✗ ") + err.Error() + "\n")
		return b.String()
	}
	res := m.results[req]
	if res == nil {
		return t.Dim.Render("not run yet · press enter to send it, f to run the whole file")
	}
	if m.showCurl {
		return t.Bold.Render("curl") + "\n\n" + m.curlFor(req) + "\n\n" + t.Dim.Render("c hides this")
	}
	var b strings.Builder
	b.WriteString(output.RequestLine(t, res) + "\n")
	if m.showHeaders {
		b.WriteString(output.RequestDetail(t, res))
	}
	b.WriteString(output.StatusLine(t, res))
	if res.Response == nil {
		return b.String()
	}
	if m.showHeaders {
		b.WriteString(output.ResponseHeaders(t, res))
	} else {
		b.WriteString(t.Dim.Render("H shows headers") + "\n")
	}
	if raw := res.Raw(); len(raw.Body) > 0 {
		b.WriteString("\n" + output.RenderBody(t, raw.Body) + "\n")
	}
	if summary := output.Checks(t, res, width-4, false); summary != "" {
		b.WriteString("\n" + summary)
	}
	return b.String()
}

func (m *Model) curlFor(req interface{ ID() string }) string {
	if m.inflight != nil {
		return m.theme.Dim.Render("(available once the run finishes)")
	}
	r := m.selected
	resolved, err := m.runner.Resolve(r)
	if err != nil {
		return m.theme.Fail.Render("✗ ") + err.Error()
	}
	if d := m.descs[r]; d != nil && !d.Ready {
		return m.theme.Warn.Render("some variables are missing; the command below is incomplete") + "\n\n" + curlexport.Command(resolved)
	}
	return curlexport.Command(resolved)
}

func (m *Model) renderChecks(width int) string {
	t := m.theme
	res := m.results[m.selected]
	if res == nil {
		if err := m.errs[m.selected]; err != nil {
			return t.Fail.Render("✗ ") + err.Error()
		}
		d := m.descs[m.selected]
		var b strings.Builder
		b.WriteString(t.Dim.Render("not run yet · these are the checks it declares") + "\n")
		if d != nil {
			for _, a := range d.Asserts {
				b.WriteString("\n  " + a)
			}
			for _, c := range d.Captures {
				b.WriteString("\n  " + t.Capture.Render("↳") + " " + c)
			}
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString(output.RequestLine(t, res) + "\n")
	b.WriteString(output.StatusLine(t, res) + "\n")
	checks := output.Checks(t, res, width-4, true)
	if checks == "" {
		checks = t.Dim.Render("no assertions or captures on this request") + "\n"
	}
	b.WriteString(checks)
	return b.String()
}

func (m *Model) renderSession(width int) string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.Bold.Render("session · "+m.envLabel()) + "\n")
	if m.runner.Session == nil {
		b.WriteString(t.Dim.Render("session disabled by --no-session") + "\n")
		return b.String()
	}
	if len(m.session) == 0 {
		b.WriteString(t.Dim.Render("nothing captured yet · run a request with # @capture") + "\n")
		return b.String()
	}
	for _, k := range sortedKeys(m.session) {
		v := m.session[k]
		switch {
		case strings.HasPrefix(k, "$oauth2:") || strings.HasPrefix(k, "$exec:"):
			v = auth.DescribeCached(v, time.Now())
		case m.cfg.Redact:
			v = "***"
		default:
			v = output.Truncate(v, width-len(k)-8)
		}
		b.WriteString(fmt.Sprintf("  %s %s = %s\n", t.Capture.Render("↳"), k, v))
	}
	b.WriteString("\n" + t.Dim.Render("x clears these values for this environment") + "\n")
	return b.String()
}

func (m *Model) renderHelp() string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.Accent.Render("apic is epic") + t.Dim.Render(" · keys") + "\n\n")
	for _, col := range m.keys.columns() {
		for _, k := range col {
			b.WriteString(fmt.Sprintf("  %-13s %s\n", t.Bold.Render(k.name), k.help))
		}
		b.WriteString("\n")
	}
	b.WriteString(t.Dim.Render("esc or ? closes this"))
	return b.String()
}
