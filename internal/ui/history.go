package ui

import (
	"fmt"
	"strings"

	"github.com/dataGriff/api-caller/internal/history"
	"github.com/dataGriff/api-caller/internal/output"
)

// histView is the history tab's content for one request and environment,
// read from disk once and kept until a run, a reload or another request
// or environment changes what it would show.
type histView struct {
	key      string
	entries  []history.Entry
	from, to history.Entry
	changes  []history.Change
	err      error
}

// historyView returns the selected request's history, reading it when
// the cached view is stale.
func (m *Model) historyView() *histView {
	key := fmt.Sprintf("%s|%s|%d", m.selected.ID(), m.env, m.histSeq)
	if m.hist != nil && m.hist.key == key {
		return m.hist
	}
	p := m.runner.Project
	s := history.New(p.Root, p.Config.History)
	v := &histView{key: key}
	m.hist = v
	if m.selected.Name == "" {
		return v
	}
	if v.entries, v.err = s.List(m.env, m.selected.Name); v.err != nil || len(v.entries) < 2 {
		return v
	}
	if v.from, v.err = s.Get(m.env, m.selected.Name, 2); v.err != nil {
		return v
	}
	if v.to, v.err = s.Get(m.env, m.selected.Name, 1); v.err != nil {
		return v
	}
	v.changes, v.err = history.Compare(v.from.Result, v.to.Result)
	return v
}

func (m *Model) renderHistory(width int) string {
	t := m.theme
	p := m.runner.Project
	var b strings.Builder
	head := "history · " + m.envLabel()
	if p.Config.History > 0 {
		head += t.Dim.Render(fmt.Sprintf(" · keeps the last %d", p.Config.History))
	}
	b.WriteString(t.Bold.Render(head) + "\n")
	if m.selected.Name == "" {
		b.WriteString(t.Dim.Render("history keeps named requests only · give this one a # @name") + "\n")
		return b.String()
	}
	v := m.historyView()
	if v.err != nil {
		b.WriteString(t.Fail.Render("✗ ") + v.err.Error() + "\n")
		return b.String()
	}
	switch {
	case m.runner.Opts.NoSession:
		b.WriteString(t.Dim.Render("--no-session: this run records nothing") + "\n")
	case p.Config.History <= 0:
		b.WriteString(t.Dim.Render("history is off · set history: 20 in apic.yaml to keep the last 20 responses") + "\n")
	}
	if len(v.entries) == 0 {
		b.WriteString(t.Dim.Render("nothing recorded yet · press enter to send it") + "\n")
		return b.String()
	}
	b.WriteString(output.HistoryEntries(t, v.entries))
	if len(v.entries) < 2 {
		b.WriteString("\n" + t.Dim.Render("run it again to see what changes") + "\n")
		return b.String()
	}
	b.WriteString("\n" + t.Bold.Render("what changed") + t.Dim.Render(fmt.Sprintf(" · #%d → #%d", v.from.Index, v.to.Index)) + "\n")
	if len(v.changes) == 0 {
		b.WriteString(t.Dim.Render("no changes in status or body") + "\n")
		return b.String()
	}
	b.WriteString(output.Changes(t, v.changes, width-4))
	b.WriteString(t.Dim.Render(fmt.Sprintf("apic history diff %s for the same in a terminal", m.selected.Name)) + "\n")
	return b.String()
}
