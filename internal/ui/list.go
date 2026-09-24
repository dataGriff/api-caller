package ui

import (
	"fmt"
	"strconv"
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
		if it.req != nil && it.file == sel.File.Path && !it.req.Disabled() {
			out = append(out, it.req)
		}
	}
	return out
}

func (m *Model) allRequests() []*httpfile.Request {
	var out []*httpfile.Request
	for _, it := range m.items {
		if it.req != nil && !it.req.Disabled() {
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
	rolls := m.rollups(v)
	for i := start; i < len(v) && i < start+rows; i++ {
		lines = append(lines, ansi.Truncate(m.renderRow(v[i], i == m.cursor, width, rolls), width, "…"))
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

// renderRow draws one row: a file heading with its rollup, or a request
// with its mark, method, id, description and — once it has run — the status
// and how long it took, pushed to the right edge.
func (m *Model) renderRow(it item, cursor bool, width int, rolls map[string]rollup) string {
	t := m.theme
	if it.req == nil {
		return alignRight(t.Accent.Render(it.file), m.renderRollup(rolls[it.file]), width)
	}
	r := it.req
	prefix := "  "
	if cursor {
		prefix = t.Accent.Render("▸ ")
	}
	id := r.ID()
	switch {
	case cursor:
		id = t.Bold.Render(id)
	case r.Disabled():
		// Kept out of runs of the file and of everything; r still sends it.
		id = t.Dim.Render(id)
	}
	head := prefix + m.rowMark(r) + " " + t.Method(fmt.Sprintf("%-6s", r.Method)) + " " + id

	badge, room := m.rowLayout(r, ansi.StringWidth(head), width)
	if r.Description != "" && room >= minDesc {
		// Truncate adds an ellipsis of its own, and the description is
		// preceded by a space, so two columns of the budget are spoken for.
		head += " " + t.Dim.Render(output.Truncate(r.Description, room-2))
	}
	return alignRight(head, badge, width)
}

const (
	rowGap  = 2  // blank columns between a row's text and its right-hand badge
	minDesc = 10 // the narrowest description budget worth spending a row on
)

// rowLayout decides how much of a row survives its width. A description
// that would be cut to a stub is worth less than the timing beside it, and
// both are worth less than the status code, so the row sheds them in that
// order. It returns the badge to draw and the columns left for everything
// between the id and it.
func (m *Model) rowLayout(r *httpfile.Request, headW, width int) (badge string, room int) {
	desc := 0
	if r.Description != "" {
		desc = minDesc
	}
	full, short := m.rowBadge(r, false), m.rowBadge(r, true)
	for _, try := range []struct {
		badge string
		desc  int
	}{{full, desc}, {short, desc}, {full, 0}, {short, 0}, {"", 0}} {
		room := width - headW - ansi.StringWidth(try.badge) - rowGap
		if room >= try.desc {
			return try.badge, room
		}
	}
	return "", width - headW
}

// alignRight pads left so that right ends at width, and drops right when
// the two cannot both fit.
func alignRight(left, right string, width int) string {
	if right == "" {
		return left
	}
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// rowBadge is the outcome of a request once it has run: the status code and
// its round trip, or "error" when no response came back at all. The short
// form drops the timing, for a pane with no room for it.
func (m *Model) rowBadge(r *httpfile.Request, short bool) string {
	t := m.theme
	if rs := m.inflight; rs != nil && rs.reqs[rs.idx] == r {
		return "" // the spinner in the mark says everything
	}
	res := m.results[r]
	if res == nil || res.Response == nil {
		if m.errs[r] != nil || res != nil {
			if short {
				return t.Fail.Render("err")
			}
			return t.Fail.Render("error")
		}
		return ""
	}
	code := t.StatusCode(res.Response.Status)
	if short {
		return code
	}
	return code + " " + t.Dim.Render(fmt.Sprintf("%dms", res.Response.DurationMs))
}

// rollup counts how the requests of one file have fared so far.
type rollup struct{ total, pass, fail int }

// rollups tallies the visible rows per file, so a heading can summarise the
// same requests the filter left on screen.
func (m *Model) rollups(v []item) map[string]rollup {
	out := map[string]rollup{}
	for _, it := range v {
		if it.req == nil {
			continue
		}
		r := out[it.file]
		r.total++
		switch {
		case m.errs[it.req] != nil:
			r.fail++
		case m.results[it.req] != nil:
			if m.results[it.req].OK {
				r.pass++
			} else {
				r.fail++
			}
		}
		out[it.file] = r
	}
	return out
}

// renderRollup is the right-hand side of a file heading: how many requests
// it holds, and how many have passed or failed once any of them has run.
func (m *Model) renderRollup(r rollup) string {
	t := m.theme
	if r.total == 0 {
		return ""
	}
	if r.pass == 0 && r.fail == 0 {
		return t.Dim.Render(strconv.Itoa(r.total))
	}
	var parts []string
	if r.pass > 0 {
		parts = append(parts, t.OK.Render("✓"+strconv.Itoa(r.pass)))
	}
	if r.fail > 0 {
		parts = append(parts, t.Fail.Render("✗"+strconv.Itoa(r.fail)))
	}
	if rest := r.total - r.pass - r.fail; rest > 0 {
		parts = append(parts, t.Dim.Render("·"+strconv.Itoa(rest)))
	}
	return strings.Join(parts, " ")
}

// rowMark is the one-character state of a request: running, passed, failed,
// disabled, ready or not ready.
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
	if r.Disabled() {
		return t.Dim.Render("-")
	}
	if d := m.descs[r]; d != nil && !d.Ready {
		return t.Warn.Render("○")
	}
	return t.Dim.Render("●")
}
