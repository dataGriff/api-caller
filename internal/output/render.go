package output

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/runner"
)

// Options controls how a result is rendered for humans.
type Options struct {
	Verbose bool // request headers and body, response headers
	Width   int  // where long values are cut; 0 means 80
}

// RequestLine renders "METHOD url" with the URL masked when redacting.
func RequestLine(t Theme, res *runner.Result) string {
	return t.Method(res.Request.Method) + " " + t.URL.Render(res.Request.DisplayURL(res.Redact))
}

// RequestDetail renders the request headers and body (verbose mode).
func RequestDetail(t Theme, res *runner.Result) string {
	var b strings.Builder
	for _, h := range res.Request.DisplayHeaders(res.Redact) {
		fmt.Fprintf(&b, "%s %s\n", t.Header.Render(h.Name+":"), h.Value)
	}
	if res.Request.TLS != nil {
		fmt.Fprintf(&b, "%s %s\n", t.Dim.Render("tls:"), res.Request.TLS)
	}
	if res.Request.Proxy != nil {
		fmt.Fprintf(&b, "%s %s\n", t.Dim.Render("proxy:"), res.Request.Proxy)
	}
	if n := res.AuthNote(); n != "" {
		fmt.Fprintf(&b, "%s %s\n", t.Dim.Render("auth:"), n)
	}
	if body := res.Request.DisplayBody(res.Redact); body != "" {
		fmt.Fprintf(&b, "\n%s\n", strings.TrimRight(body, "\n"))
	}
	return b.String()
}

// StatusLine renders "200 OK · 87 ms · 412 B"; when the request never got a
// response it renders the errors instead.
func StatusLine(t Theme, res *runner.Result) string {
	if res.Response == nil {
		var b strings.Builder
		for _, e := range res.Errors {
			fmt.Fprintf(&b, "%s %s\n", t.Fail.Render("✗"), e)
		}
		return b.String()
	}
	raw := res.Raw()
	status := t.Status(raw.Status, raw.StatusText)
	line := fmt.Sprintf("%s %s %s %s %s", status, t.Dim.Render("·"), t.Latency(res.Response.DurationMs), t.Dim.Render("·"), t.Dim.Render(Size(res.Response.Size)))
	if res.Attempts > 1 {
		line += " " + t.Dim.Render(fmt.Sprintf("· %d attempts", res.Attempts))
	}
	return line + "\n"
}

// Timings renders where the round trip went, when the response carries a
// breakdown: "dns 12 ms · connect 18 ms · tls 41 ms · ttfb 60 ms · total
// 87 ms · new connection".
func Timings(t Theme, res *runner.Result) string {
	if res.Response == nil || res.Response.Timings == nil {
		return ""
	}
	tm := res.Response.Timings
	conn := "new connection"
	if tm.Reused {
		conn = "reused connection"
	}
	return t.Dim.Render(fmt.Sprintf("dns %d ms · connect %d ms · tls %d ms · ttfb %d ms · total %d ms · %s", tm.DNSMs, tm.ConnectMs, tm.TLSMs, tm.TTFBMs, tm.TotalMs, conn)) + "\n"
}

// Attempt renders the progress line printed after a failed attempt of a
// request that is being retried.
func Attempt(t Theme, p runner.Progress) string {
	return t.Dim.Render(fmt.Sprintf("attempt %d/%d · %s", p.Attempt, p.Max, p.Failure)) + "\n"
}

// ResponseHeaders renders the response headers sorted and lower-cased, with
// sensitive values masked and every value masked when redacting.
func ResponseHeaders(t Theme, res *runner.Result) string {
	headers := res.Response.DisplayHeaders(res.Redact)
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s %s\n", t.Header.Render(strings.ToLower(k)+":"), headers[k])
	}
	return b.String()
}

// RenderBody pretty-prints and highlights a JSON body; other bodies are
// returned as they are. The result has no trailing newline.
func RenderBody(t Theme, raw []byte) string {
	pretty := bytes.TrimRight(prettyJSON(raw), "\n")
	if isJSON(raw) {
		return HighlightJSON(t, string(pretty))
	}
	return string(pretty)
}

// Checks renders the assertions, captures and errors of a result. With
// expected set, failed assertions show the expected value as well as the
// actual one.
func Checks(t Theme, res *runner.Result, width int, expected bool) string {
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	for _, a := range res.DisplayAsserts() {
		switch {
		case a.Error != "":
			fmt.Fprintf(&b, "%s %s %s\n", t.Fail.Render("✗"), a.Expr, t.Dim.Render("("+a.Error+")"))
		case a.Pass:
			fmt.Fprintf(&b, "%s %s\n", t.OK.Render("✓"), a.Expr)
		default:
			fmt.Fprintf(&b, "%s %s %s\n", t.Fail.Render("✗"), a.Expr, t.Dim.Render(fmt.Sprintf("(actual: %s)", Truncate(a.Actual, width))))
			if expected {
				fmt.Fprintf(&b, "  %s %s\n", t.Dim.Render("expected:"), Truncate(a.Expected, width))
			}
		}
	}
	if res.SavedTo != "" {
		fmt.Fprintf(&b, "%s %s\n", t.Capture.Render("↳"), t.Dim.Render("saved to ")+res.SavedTo)
	}
	captures := res.DisplayCaptures()
	names := make([]string, 0, len(captures))
	for n := range captures {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(&b, "%s %s = %s\n", t.Capture.Render("↳"), n, Truncate(captures[n], width))
	}
	if res.Response != nil {
		for _, e := range res.Errors {
			fmt.Fprintf(&b, "%s %s\n", t.Fail.Render("✗"), e)
		}
	}
	return b.String()
}

// Result renders the whole report for one request, as `apic run` prints it.
func Result(t Theme, res *runner.Result, o Options) string {
	var b strings.Builder
	b.WriteString(RequestLine(t, res) + "\n")
	if o.Verbose {
		b.WriteString(RequestDetail(t, res))
		b.WriteString("\n")
	}
	b.WriteString(StatusLine(t, res))
	if res.Response == nil {
		return b.String()
	}
	if o.Verbose {
		b.WriteString(Timings(t, res))
		b.WriteString(ResponseHeaders(t, res))
	}
	if body := res.DisplayRawBody(); len(body) > 0 {
		b.WriteString("\n" + BodyOrSummary(t, res, body) + "\n")
	}
	if checks := Checks(t, res, o.Width, false); checks != "" {
		b.WriteString("\n" + checks)
	}
	return b.String()
}

// BodyOrSummary renders a body, or for one that is not text, a line with
// its size and content type instead of the bytes.
func BodyOrSummary(t Theme, res *runner.Result, body []byte) string {
	if res.Response == nil || res.Response.BodyEncoding != runner.Base64 {
		return RenderBody(t, body)
	}
	ct := "unknown content type"
	for k, v := range res.Response.Headers {
		if strings.EqualFold(k, "content-type") {
			ct = v
		}
	}
	return t.Dim.Render(fmt.Sprintf("(binary body · %s · %s; save it with >> file or --output)", Size(len(body)), ct))
}

// SummaryTable renders one row per request followed by the totals line, for
// flows of more than one request.
func SummaryTable(t Theme, results []*runner.Result) string {
	var b strings.Builder
	nameW := 0
	for _, r := range results {
		if w := lipgloss.Width(resultName(r)); w > nameW {
			nameW = w
		}
	}
	var total int64
	for _, r := range results {
		name := resultName(r)
		pad := strings.Repeat(" ", nameW-lipgloss.Width(name))
		status, latency := t.Dim.Render("   —"), ""
		if r.Response != nil {
			status = t.Status(r.Response.Status, "")
			status = strings.TrimRight(status, " ")
			latency = t.Latency(r.Response.DurationMs)
			total += r.Response.DurationMs
		}
		fmt.Fprintf(&b, "%s %s%s  %s  %s", t.Mark(r.OK), name, pad, status, latency)
		if !r.OK {
			if why := firstProblem(r); why != "" {
				fmt.Fprintf(&b, "  %s", t.Dim.Render(why))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\n" + SummaryLine(t, results) + t.Dim.Render(fmt.Sprintf(" · %d requests · %d ms", len(results), total)) + "\n")
	return b.String()
}

// SummaryLine renders "3 passed" or "1 failed, 3 passed".
func SummaryLine(t Theme, results []*runner.Result) string {
	passed := 0
	for _, r := range results {
		if r.OK {
			passed++
		}
	}
	failed := len(results) - passed
	line := fmt.Sprintf("%d passed", passed)
	if failed > 0 {
		return t.Fail.Render(fmt.Sprintf("%d failed", failed)) + ", " + line
	}
	return t.OK.Render(line)
}

func resultName(r *runner.Result) string {
	if r.Request.Name != "" {
		return r.Request.Name
	}
	return fmt.Sprintf("%s:%d", r.Request.File, r.Request.Line)
}

// firstProblem is the one-line reason a result is not OK.
func firstProblem(r *runner.Result) string {
	for _, a := range r.DisplayAsserts() {
		if a.Error != "" {
			return a.Expr + " (" + a.Error + ")"
		}
		if !a.Pass {
			return fmt.Sprintf("%s (actual: %s)", a.Expr, Truncate(a.Actual, 40))
		}
	}
	if len(r.Errors) > 0 {
		return Truncate(r.Errors[0], 60)
	}
	return ""
}

// Describe renders what `apic describe` shows. headers are the request's raw
// headers in file order.
func Describe(t Theme, d *runner.Description, headers []httpfile.Header) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", t.Method(d.Method), t.URL.Render(d.URLTemplate))
	if d.Description != "" {
		b.WriteString(d.Description + "\n")
	}
	fmt.Fprintf(&b, "%s %s:%d\n", t.Dim.Render("file:"), d.File, d.Line)
	if d.ID != d.Name {
		fmt.Fprintf(&b, "%s %s\n", t.Dim.Render("id:  "), d.ID)
	}
	if len(headers) > 0 {
		b.WriteString(Section(t, "headers"))
		for _, h := range headers {
			fmt.Fprintf(&b, "  %s %s\n", t.Header.Render(h.Name+":"), h.Value)
		}
	}
	if d.Body != "" {
		b.WriteString(Section(t, "body"))
		b.WriteString(Indent(d.Body) + "\n")
	}
	if d.Auth != "" {
		b.WriteString(Section(t, "auth"))
		fmt.Fprintf(&b, "  %s %s\n", d.Auth, t.Dim.Render("("+d.AuthSource+")"))
	}
	if d.BodyFile != "" {
		b.WriteString(Section(t, "body file"))
		b.WriteString("  " + d.BodyFile + "\n")
	}
	if d.SaveTo != "" {
		b.WriteString(Section(t, "saves the response to"))
		b.WriteString("  " + d.SaveTo + "\n")
	}
	if d.TLS != nil {
		b.WriteString(Section(t, "tls"))
		b.WriteString("  " + d.TLS.String() + "\n")
	}
	if d.Proxy != nil {
		b.WriteString(Section(t, "proxy"))
		b.WriteString("  " + d.Proxy.String() + "\n")
	}
	b.WriteString(Section(t, "variables"))
	if len(d.Variables) == 0 {
		b.WriteString("  (none)\n")
	}
	b.WriteString(Variables(t, d.Variables, true))
	if len(d.Steps) > 0 {
		b.WriteString(Section(t, "steps"))
		for _, st := range d.Steps {
			b.WriteString("  " + st + "\n")
		}
	}
	if len(d.Captures) > 0 {
		b.WriteString(Section(t, "captures"))
		for _, c := range d.Captures {
			b.WriteString("  " + t.Capture.Render("↳") + " " + c + "\n")
		}
	}
	if len(d.Asserts) > 0 {
		b.WriteString(Section(t, "asserts"))
		for _, x := range d.Asserts {
			b.WriteString("  " + x + "\n")
		}
	}
	b.WriteString("\n" + Readiness(t, d) + "\n")
	return b.String()
}

// Readiness renders the boxed verdict at the end of a description.
func Readiness(t Theme, d *runner.Description) string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if d.Ready {
		return box.BorderForeground(lipgloss.Color("42")).Render(t.OK.Render("ready") + " " + t.URL.Render(d.URL))
	}
	var missing []string
	for _, v := range d.Variables {
		if v.Missing {
			missing = append(missing, "{{"+v.Name+"}}")
		}
	}
	return box.BorderForeground(lipgloss.Color("214")).Render(t.Warn.Render("not ready") + " " + t.Dim.Render("missing "+strings.Join(missing, ", ")))
}

// Variables renders a variable table: name, value (masked when secret) and
// source, with hints for missing ones.
func Variables(t Theme, vars []runner.VarInfo, hints bool) string {
	nameW := 0
	for _, v := range vars {
		if w := len(v.Name) + 2; w > nameW {
			nameW = w
		}
	}
	var b strings.Builder
	for _, v := range vars {
		pad := strings.Repeat(" ", nameW-len(v.Name)-2)
		switch {
		case v.Missing && v.RefRuns && hints:
			fmt.Fprintf(&b, "  %s%s  %s  %s\n", t.Warn.Render("○ "+v.Name), pad, "missing", fmt.Sprintf("captured by %s, which # @ref runs first", v.CapturedBy))
		case v.Missing && v.CapturedBy != "" && hints:
			fmt.Fprintf(&b, "  %s%s  %s  %s\n", t.Fail.Render("✗ "+v.Name), pad, "missing", fmt.Sprintf("captured by %s — run `apic run %s` first", v.CapturedBy, v.CapturedBy))
		case v.Missing && hints:
			fmt.Fprintf(&b, "  %s%s  %s  %s\n", t.Fail.Render("✗ "+v.Name), pad, "missing", "pass --var "+v.Name+"=...")
		case v.Missing:
			fmt.Fprintf(&b, "  %s%s  %s\n", t.Fail.Render("✗ "+v.Name), pad, "missing")
		default:
			fmt.Fprintf(&b, "  %s%s  %s  %s\n", t.OK.Render("✓ "+v.Name), pad, Mask(v), t.Dim.Render(v.Source))
		}
	}
	return b.String()
}

// Mask returns a variable's value, or *** when it is a secret.
func Mask(v runner.VarInfo) string {
	if v.Secret {
		return runner.Masked
	}
	return Truncate(v.Value, 60)
}

// Section renders a bold section title preceded by a blank line.
func Section(t Theme, title string) string {
	return "\n" + t.Bold.Render(title) + "\n"
}

// Indent prefixes every line with two spaces.
func Indent(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n  ")
}

// Truncate flattens newlines and cuts s to n runes, appending an ellipsis.
func Truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Size formats a byte count.
func Size(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	}
}
