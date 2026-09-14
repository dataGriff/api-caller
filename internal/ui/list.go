package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/output"
)

// item is one row of the left pane: a file heading (req == nil) or a request.
type item struct {
	file string
	req  *httpfile.Request
}

func buildItems(reqs []*httpfile.Request) []item {
	var items []item
	last := ""
	for _, r := range reqs {
		if r.File.Path != last {
			items = append(items, item{file: r.File.Path})
			last = r.File.Path
		}
		items = append(items, item{file: r.File.Path, req: r})
	}
	return items
}

func (it item) matches(filter string) bool {
	if filter == "" {
		return true
	}
	f := strings.ToLower(filter)
	if it.req == nil {
		return false
	}
	r := it.req
	return strings.Contains(strings.ToLower(r.ID()), f) ||
		strings.Contains(strings.ToLower(r.URL), f) ||
		strings.Contains(strings.ToLower(r.Description), f) ||
		strings.Contains(strings.ToLower(r.Method), f) ||
		strings.Contains(strings.ToLower(r.File.Path), f)
}

// visible returns the rows after filtering; file headings stay only when a
// request under them matches.
func (m *Model) visible() []item {
	if m.filter == "" {
		return m.items
	}
	var out []item
	for i, it := range m.items {
		if it.req == nil {
			for _, sub := range m.items[i+1:] {
				if sub.req == nil {
					break
				}
				if sub.matches(m.filter) {
					out = append(out, it)
					break
				}
			}
			continue
		}
		if it.matches(m.filter) {
			out = append(out, it)
		}
	}
	return out
}

func (m *Model) selectedReq() *httpfile.Request {
	v := m.visible()
	if m.cursor < 0 || m.cursor >= len(v) {
		return nil
	}
	return v[m.cursor].req
}

// clampCursor keeps the cursor on a request row.
func (m *Model) clampCursor() {
	v := m.visible()
	if len(v) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(v) {
		m.cursor = len(v) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	for m.cursor < len(v) && v[m.cursor].req == nil {
		m.cursor++
	}
	if m.cursor >= len(v) {
		for m.cursor > 0 && v[m.cursor].req == nil {
			m.cursor--
		}
	}
}

func (m *Model) move(delta int) {
	v := m.visible()
	i := m.cursor
	for {
		i += delta
		if i < 0 || i >= len(v) {
			return
		}
		if v[i].req != nil {
			m.cursor = i
			m.selected = v[i].req
			return
		}
	}
}

func (m *Model) moveTo(first bool) {
	v := m.visible()
	if first {
		m.cursor = 0
		m.clampCursor()
	} else {
		m.cursor = len(v) - 1
		for m.cursor > 0 && v[m.cursor].req == nil {
			m.cursor--
		}
	}
	m.selected = m.selectedReq()
}

// fileRequests returns every request in the file of the selected request.
func (m *Model) fileRequests() []*httpfile.Request {
	sel := m.selectedReq()
	if sel == nil {
		return nil
	}
	var out []*httpfile.Request
	for _, it := range m.items {
		if it.req != nil && it.file == sel.File.Path {
			out = append(out, it.req)
		}
	}
	return out
}

func (m *Model) allRequests() []*httpfile.Request {
	var out []*httpfile.Request
	for _, it := range m.items {
		if it.req != nil {
			out = append(out, it.req)
		}
	}
	return out
}

// renderList draws the left pane: a title or the filter prompt, then a
// window of rows around the cursor.
func (m *Model) renderList(width, height int) string {
	t := m.theme
	v := m.visible()
	title := t.Bold.Render(fmt.Sprintf("requests (%d)", m.countRequests(v)))
	if m.filtering {
		title = t.Accent.Render("/") + m.filter + t.Accent.Render("▏")
	} else if m.filter != "" {
		title = t.Bold.Render(fmt.Sprintf("requests (%d)", m.countRequests(v))) + t.Dim.Render(" /"+m.filter)
	}
	lines := []string{ansi.Truncate(title, width, "…")}
	rows := height - 1
	if rows < 1 {
		rows = 1
	}
	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	if len(v) == 0 {
		lines = append(lines, t.Dim.Render("  no requests match"))
	}
	for i := start; i < len(v) && i < start+rows; i++ {
		lines = append(lines, ansi.Truncate(m.renderRow(v[i], i == m.cursor, width), width, "…"))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	pad := func(s string) string {
		w := ansi.StringWidth(s)
		if w < width {
			return s + strings.Repeat(" ", width-w)
		}
		return s
	}
	for i := range lines {
		lines[i] = pad(lines[i])
	}
	return strings.Join(lines, "\n")
}

func (m *Model) countRequests(v []item) int {
	n := 0
	for _, it := range v {
		if it.req != nil {
			n++
		}
	}
	return n
}

func (m *Model) renderRow(it item, cursor bool, width int) string {
	t := m.theme
	if it.req == nil {
		return t.Accent.Render(it.file)
	}
	r := it.req
	mark := m.rowMark(r)
	prefix := "  "
	if cursor {
		prefix = t.Accent.Render("▸ ")
	}
	method := t.Method(fmt.Sprintf("%-6s", r.Method))
	id := r.ID()
	if cursor {
		id = t.Bold.Render(id)
	}
	desc := ""
	if r.Description != "" {
		desc = " " + t.Dim.Render(output.Truncate(r.Description, width))
	}
	return prefix + mark + " " + method + " " + id + desc
}

// rowMark is the one-character state of a request: running, passed, failed,
// ready or not ready.
func (m *Model) rowMark(r *httpfile.Request) string {
	t := m.theme
	if rs := m.inflight; rs != nil && rs.reqs[rs.idx] == r {
		return m.theme.Accent.Render(m.spin.view())
	}
	if err := m.errs[r]; err != nil {
		return t.Fail.Render("✗")
	}
	if res := m.results[r]; res != nil {
		return t.Mark(res.OK)
	}
	if d := m.descs[r]; d != nil && !d.Ready {
		return t.Warn.Render("○")
	}
	return t.Dim.Render("●")
}
