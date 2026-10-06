package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/dataGriff/api-caller/internal/history"
)

// HistoryTime is how history entries show their time: local, to the
// second.
func HistoryTime(at time.Time) string { return at.Local().Format("2006-01-02 15:04:05") }

// HistoryEntries renders a request's history, newest first: one row per
// entry with its number, time, status, duration and size.
func HistoryEntries(t Theme, entries []history.Entry) string {
	var b strings.Builder
	for _, e := range entries {
		mark := t.OK.Render("✓")
		if !e.OK {
			mark = t.Fail.Render("✗")
		}
		fmt.Fprintf(&b, "  %s %s  %s  %s %s %s %s %s\n", t.Dim.Render(fmt.Sprintf("#%-2d", e.Index)), HistoryTime(e.Time), mark,
			t.Status(e.Status, e.StatusText), t.Dim.Render("·"), t.Latency(e.DurationMs), t.Dim.Render("·"), t.Dim.Render(Size(e.Size)))
	}
	return b.String()
}

// HistoryEntry names one entry: "#2 (2026-10-06 10:00:01, 200)".
func HistoryEntry(e history.Entry) string {
	return fmt.Sprintf("#%d (%s, %d)", e.Index, HistoryTime(e.Time), e.Status)
}

// Changes renders the differences between two responses, one per line:
// ~ for a changed value, + for an added one, - for a removed one. Values
// are cut at width.
func Changes(t Theme, changes []history.Change, width int) string {
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	for _, c := range changes {
		from, to := Truncate(string(c.From), width), Truncate(string(c.To), width)
		switch c.Op {
		case history.Added:
			fmt.Fprintf(&b, "%s %s: %s\n", t.OK.Render("+"), c.Path, to)
		case history.Removed:
			fmt.Fprintf(&b, "%s %s: %s\n", t.Fail.Render("-"), c.Path, from)
		default:
			if from == "" && to == "" {
				fmt.Fprintf(&b, "%s %s %s\n", t.Warn.Render("~"), c.Path, t.Dim.Render("differs"))
				continue
			}
			fmt.Fprintf(&b, "%s %s: %s %s %s\n", t.Warn.Render("~"), c.Path, from, t.Dim.Render("→"), to)
		}
	}
	return b.String()
}
