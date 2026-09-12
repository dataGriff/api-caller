// Package output renders run results for humans (coloured, readable) and for
// machines (one JSON object per result).
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/dataGriff/api-caller/internal/runner"
)

var (
	styleMethod  = lipgloss.NewStyle().Bold(true)
	styleURL     = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	styleFail    = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	styleDim     = lipgloss.NewStyle().Faint(true)
	styleHeader  = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	styleCapture = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
)

// JSON writes one result as a single JSON line.
func JSON(w io.Writer, res *runner.Result) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(res)
}

// Body writes only the response body, pretty-printed when it is JSON.
func Body(w io.Writer, res *runner.Result) {
	if res.Raw() == nil {
		return
	}
	_, _ = w.Write(prettyJSON(res.Raw().Body))
	_, _ = io.WriteString(w, "\n")
}

// Human writes a readable report of a result.
func Human(w io.Writer, res *runner.Result, verbose bool) {
	req := res.Request
	fmt.Fprintf(w, "%s %s\n", styleMethod.Render(req.Method), styleURL.Render(req.URL))
	if verbose {
		for _, h := range req.Headers {
			fmt.Fprintf(w, "%s %s\n", styleHeader.Render(h.Name+":"), h.Value)
		}
		if req.Body != "" {
			fmt.Fprintf(w, "\n%s\n", strings.TrimRight(req.Body, "\n"))
		}
		fmt.Fprintln(w)
	}
	if res.Response == nil {
		for _, e := range res.Errors {
			fmt.Fprintf(w, "%s %s\n", styleFail.Render("✗"), e)
		}
		return
	}
	raw := res.Raw()
	status := fmt.Sprintf("%d %s", raw.Status, raw.StatusText)
	switch {
	case raw.Status < 300:
		status = styleOK.Render(status)
	case raw.Status < 400:
		status = styleWarn.Render(status)
	default:
		status = styleFail.Render(status)
	}
	fmt.Fprintf(w, "%s %s\n", status, styleDim.Render(fmt.Sprintf("· %d ms · %s", res.Response.DurationMs, size(res.Response.Size))))
	if verbose {
		keys := make([]string, 0, len(raw.Headers))
		for k := range raw.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "%s %s\n", styleHeader.Render(strings.ToLower(k)+":"), strings.Join(raw.Headers[k], ", "))
		}
	}
	if len(raw.Body) > 0 {
		fmt.Fprintf(w, "\n%s\n", bytes.TrimRight(prettyJSON(raw.Body), "\n"))
	}
	if len(res.Asserts)+len(res.Captures)+len(res.Errors) > 0 {
		fmt.Fprintln(w)
	}
	for _, a := range res.Asserts {
		switch {
		case a.Error != "":
			fmt.Fprintf(w, "%s %s %s\n", styleFail.Render("✗"), a.Expr, styleDim.Render("("+a.Error+")"))
		case a.Pass:
			fmt.Fprintf(w, "%s %s\n", styleOK.Render("✓"), a.Expr)
		default:
			fmt.Fprintf(w, "%s %s %s\n", styleFail.Render("✗"), a.Expr, styleDim.Render(fmt.Sprintf("(actual: %s)", truncate(a.Actual, 80))))
		}
	}
	names := make([]string, 0, len(res.Captures))
	for n := range res.Captures {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "%s %s = %s\n", styleCapture.Render("↳"), n, truncate(res.Captures[n], 80))
	}
	for _, e := range res.Errors {
		fmt.Fprintf(w, "%s %s\n", styleFail.Render("✗"), e)
	}
}

// Summary writes a one-line flow summary.
func Summary(w io.Writer, results []*runner.Result) {
	passed := 0
	for _, r := range results {
		if r.OK {
			passed++
		}
	}
	failed := len(results) - passed
	line := fmt.Sprintf("%d passed", passed)
	if failed > 0 {
		line = styleFail.Render(fmt.Sprintf("%d failed", failed)) + ", " + line
	} else {
		line = styleOK.Render(line)
	}
	fmt.Fprintf(w, "\n%s\n", line)
}

func prettyJSON(b []byte) []byte {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || (t[0] != '{' && t[0] != '[') || !json.Valid(t) {
		return b
	}
	var out bytes.Buffer
	if err := json.Indent(&out, t, "", "  "); err != nil {
		return b
	}
	return out.Bytes()
}

func size(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
