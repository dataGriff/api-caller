package httpfile

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Diagnostic is a non-fatal problem found while parsing or validating.
//
// Line and Column are 1-based; Column counts bytes from the start of the
// line. EndLine and EndColumn mark the end of the span, exclusive, so a
// diagnostic on the word "nope" at column 12 has EndColumn 16. The columns
// are zero when the problem has no useful span (a whole file, or apic.yaml).
// Code is a stable identifier from Codes that tools can key fixes on.
type Diagnostic struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	EndColumn int    `json:"end_column,omitempty"`
	Severity  string `json:"severity"` // "error" or "warning"
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
}

func (d Diagnostic) String() string {
	if d.Column > 0 {
		return fmt.Sprintf("%s:%d:%d: %s: %s", d.Path, d.Line, d.Column, d.Severity, d.Message)
	}
	return fmt.Sprintf("%s:%d: %s: %s", d.Path, d.Line, d.Severity, d.Message)
}

// Codes are the diagnostic codes apic reports, with a one-line description
// each. They are part of the `validate --json` contract: a code never
// changes meaning, and new ones are only added.
var Codes = map[string]string{
	"orphan-directives": "directives that are not followed by a request line",
	"bad-name":          "`# @name` without a value",
	"bad-capture":       "`# @capture` that is not `name = selector`",
	"bad-assert":        "`# @assert` that is not `selector op value`",
	"bad-header":        "a line in the header section that is not `Name: value`",
	"unknown-directive": "a `# @directive` apic does not know (ignored)",
	"editor-script":     "an editor-only script or redirect block (skipped, not sent)",
	"duplicate-name":    "a request name used more than once in the project",
	"bad-auth":          "an `# @auth` spec that does not parse",
	"exec-disabled":     "`# @auth exec` without auth.allowExec in apic.yaml",
	"bad-config-auth":   "auth.default in apic.yaml does not parse",
	"bad-step":          "a `# @step` phrase that does not parse",
	"ambiguous-step":    "a `# @step` phrase that matches the same text as another step",
	"unknown-selector":  "a selector that is not status, statusText, duration, header.*, body or body.$*",
	"missing-body-file": "a `< file` body whose file does not exist",
}

// Span returns the 1-based byte columns [col, end) of sub within line, or
// zeros when sub is empty or not found. after is the offset to start
// searching from, so a value that also appears in the directive key is
// found in the right place.
func Span(line, sub string, after int) (col, end int) {
	if sub == "" || after < 0 || after > len(line) {
		return 0, 0
	}
	i := strings.Index(line[after:], sub)
	if i < 0 {
		return 0, 0
	}
	col = after + i + 1
	return col, col + len(sub)
}

// KnownDirectives are the `# @key` directives apic understands. Others are
// reported as warnings by validate but otherwise ignored.
var KnownDirectives = map[string]string{
	"name":        "request name used on the command line",
	"description": "one-line description shown by list/describe",
	"capture":     "store a response value: `# @capture name = selector`",
	"assert":      "assert on the response: `# @assert selector op value`",
	"auth":        "authentication: `# @auth bearer|basic|aws|oauth2|exec|none ...`",
	"step":        "Gherkin phrase that runs this request: `# @step a user named {name} exists`",
	"no-redirect": "do not follow redirects",
	"no-session":  "do not persist captures from this request",
	"timeout":     "per-request timeout, e.g. `10s`",
	"note":        "free text, ignored (REST Client compatibility)",
	"prompt":      "REST Client prompt, ignored (pass with --var instead)",
}

var (
	reSeparator   = regexp.MustCompile(`^###(.*)$`)
	reFileVar     = regexp.MustCompile(`^@([A-Za-z_][\w.-]*)\s*=\s*(.*)$`)
	reComment     = regexp.MustCompile(`^(?:#|//)\s?(.*)$`)
	reDirective   = regexp.MustCompile(`^@([A-Za-z][\w-]*)(?:\s+(.*))?$`)
	reRequestLine = regexp.MustCompile(`^([A-Z]+)\s+(\S.*?)(?:\s+(HTTP/[\d.]+))?\s*$`)
	// Header names are RFC 7230 tokens, so X.Correlation-ID is valid. A
	// leading `#` is the one exception: that line is a comment in this
	// dialect, so the pattern does not claim it either.
	reHeader  = regexp.MustCompile("^([!$%&'*+.^_`|~0-9A-Za-z-][!#$%&'*+.^_`|~0-9A-Za-z-]*):\\s*(.*)$")
	reCapture = regexp.MustCompile(`^([A-Za-z_][\w.-]*)\s*=\s*(.+)$`)
)

// ParseFile reads and parses a .http file from disk.
func ParseFile(path string) (*File, []Diagnostic, error) {
	data, err := os.ReadFile(path) //nolint:gosec // reading the .http file the user named is the whole job
	if err != nil {
		return nil, nil, err
	}
	f, diags := Parse(path, string(data))
	return f, diags, nil
}

// Parse parses .http content. path is used only for diagnostics and IDs.
func Parse(path, content string) (*File, []Diagnostic) {
	p := &parser{file: &File{Path: path}}
	p.parse(strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n"))
	return p.file, p.diags
}

type parser struct {
	file  *File
	diags []Diagnostic
}

type block struct {
	title string
	start int
	lines []string // raw lines with their 1-based numbers in nums
	nums  []int
}

func (p *parser) parse(lines []string) {
	// Split into blocks on `###`. The first block is implicit.
	var blocks []*block
	cur := &block{start: 1}
	for i, line := range lines {
		n := i + 1
		if m := reSeparator.FindStringSubmatch(line); m != nil {
			blocks = append(blocks, cur)
			cur = &block{title: strings.TrimSpace(m[1]), start: n}
			continue
		}
		cur.lines = append(cur.lines, line)
		cur.nums = append(cur.nums, n)
	}
	blocks = append(blocks, cur)

	idx := 0
	for _, b := range blocks {
		req := p.parseBlock(b)
		if req == nil {
			continue
		}
		idx++
		req.Index = idx
		req.File = p.file
		p.file.Requests = append(p.file.Requests, req)
	}
}

// diag records a diagnostic spanning [col, end) on line; zero columns mean
// the whole line.
func (p *parser) diag(severity, code string, line, col, end int, format string, args ...any) {
	d := Diagnostic{Path: p.file.Path, Line: line, Severity: severity, Code: code, Message: fmt.Sprintf(format, args...)}
	if col > 0 {
		d.Column, d.EndLine, d.EndColumn = col, line, end
	}
	p.diags = append(p.diags, d)
}

func (p *parser) warn(code string, line, col, end int, format string, args ...any) {
	p.diag("warning", code, line, col, end, format, args...)
}

func (p *parser) errorf(code string, line, col, end int, format string, args ...any) {
	p.diag("error", code, line, col, end, format, args...)
}

// parseBlock parses one `###` block. It returns nil when the block holds no
// request (e.g. only file variables or comments).
func (p *parser) parseBlock(b *block) *Request {
	req := &Request{Title: b.title}
	i := 0
	// Preamble: comments, directives, file variables, blank lines.
	for ; i < len(b.lines); i++ {
		line := b.lines[i]
		n := b.nums[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if m := reFileVar.FindStringSubmatch(trimmed); m != nil {
			p.file.Vars = append(p.file.Vars, Var{Name: m[1], Value: strings.TrimSpace(m[2]), Line: n})
			continue
		}
		if m := reComment.FindStringSubmatch(trimmed); m != nil {
			p.parseComment(req, line, strings.TrimSpace(m[1]), n)
			continue
		}
		break
	}
	if i >= len(b.lines) {
		if len(req.Directives) > 0 {
			first := req.Directives[0]
			col, end := Span(p.rawLine(b, first.Line), "@"+first.Key, 0)
			p.errorf("orphan-directives", first.Line, col, end, "directives without a request line")
		}
		return nil
	}

	// Request line.
	line := strings.TrimSpace(b.lines[i])
	req.Line = b.nums[i]
	if m := reRequestLine.FindStringSubmatch(line); m != nil {
		req.Method, req.URL, req.HTTPVersion = m[1], m[2], m[3]
	} else {
		req.Method, req.URL = "GET", line
		if f := strings.Fields(line); len(f) == 2 && strings.HasPrefix(f[1], "HTTP/") {
			req.URL, req.HTTPVersion = f[0], f[1]
		}
	}
	i++
	// Query continuation lines: indented lines starting with ? or &.
	for i < len(b.lines) {
		t := strings.TrimSpace(b.lines[i])
		if (strings.HasPrefix(t, "?") || strings.HasPrefix(t, "&")) && b.lines[i] != t {
			req.URL += t
			i++
			continue
		}
		break
	}

	// Headers until the first blank line.
	for ; i < len(b.lines); i++ {
		t := strings.TrimSpace(b.lines[i])
		if t == "" {
			i++
			break
		}
		if reComment.MatchString(t) {
			continue
		}
		if m := reHeader.FindStringSubmatch(t); m != nil {
			req.Headers = append(req.Headers, Header{Name: m[1], Value: m[2]})
			continue
		}
		col, end := Span(b.lines[i], t, 0)
		p.errorf("bad-header", b.nums[i], col, end, "expected a header (`Name: value`) or a blank line before the body, got %q", t)
	}

	// Body: the rest of the block, trailing blank lines trimmed. Editor-only
	// handler blocks are lifted out first so they are ignored rather than sent.
	if i < len(b.lines) {
		bodyLines, handlers := splitHandlerBlocks(b.lines[i:], b.nums[i:])
		for _, h := range handlers {
			col, end := Span(p.rawLine(b, h.line), h.text, 0)
			p.warn("editor-script", h.line, col, end, "ignoring %s (apic has no scripting; see docs/comparison.md)", h.what)
		}
		body := strings.Join(bodyLines, "\n")
		body = strings.TrimRight(body, "\n\t ")
		body = strings.TrimLeft(body, "\n")
		if t := strings.TrimSpace(body); strings.HasPrefix(t, "<@ ") || strings.HasPrefix(t, "< ") {
			req.BodyFileTemplated = strings.HasPrefix(t, "<@")
			req.BodyFile = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t, "<@"), "<"))
			// Find the line the reference sits on, for diagnostics.
			for j := i; j < len(b.lines); j++ {
				if lt := strings.TrimSpace(b.lines[j]); lt != "" {
					req.BodyFileLine = b.nums[j]
					req.BodyFileColumn, _ = Span(b.lines[j], req.BodyFile, strings.Index(b.lines[j], "<"))
					break
				}
			}
		} else {
			req.Body = body
		}
	}
	if req.Description == "" {
		req.Description = req.Title
	}
	return req
}

// parseComment handles one comment line. raw is the line as written, for
// column positions; text is the comment with its marker stripped.
func (p *parser) parseComment(req *Request, raw, text string, line int) {
	m := reDirective.FindStringSubmatch(text)
	if m == nil {
		return // plain comment
	}
	key, value := m[1], strings.TrimSpace(m[2])
	// The key's span, and the value's span searched for after the key so a
	// value that repeats the key (`# @name name`) is found in the right place.
	keyCol, keyEnd := Span(raw, "@"+key, 0)
	valCol, valEnd := Span(raw, value, keyEnd)
	col := valCol
	if col == 0 {
		col = keyCol
	}
	req.Directives = append(req.Directives, Directive{Key: key, Value: value, Line: line, Column: col})
	switch key {
	case "name":
		if value == "" {
			p.errorf("bad-name", line, keyCol, keyEnd, "@name needs a value")
		}
		req.Name = value
	case "description":
		req.Description = value
	case "capture":
		cm := reCapture.FindStringSubmatch(value)
		if cm == nil {
			p.errorf("bad-capture", line, col, valEnd, "@capture must look like `name = selector`, got %q", value)
			return
		}
		sel := strings.TrimSpace(cm[2])
		selCol, _ := Span(raw, sel, valCol-1+len(cm[1]))
		req.Captures = append(req.Captures, Capture{Name: cm[1], Selector: sel, Line: line, Column: selCol})
	case "assert":
		if value == "" {
			p.errorf("bad-assert", line, keyCol, keyEnd, "@assert needs an expression")
			return
		}
		req.Asserts = append(req.Asserts, Assert{Expr: value, Line: line, Column: valCol})
	default:
		if _, ok := KnownDirectives[key]; !ok {
			p.warn("unknown-directive", line, keyCol, keyEnd, "unknown directive @%s (ignored)", key)
		}
	}
}

// rawLine returns the text of a 1-based line number within a block, or "".
func (p *parser) rawLine(b *block, line int) string {
	for i, n := range b.nums {
		if n == line {
			return b.lines[i]
		}
	}
	return ""
}

// handlerBlock is an editor-only script block found in a request body.
type handlerBlock struct {
	line int
	what string
	text string // the trimmed first line of the block, for its span
}

// splitHandlerBlocks removes the response-handler and pre-request-script
// blocks that VS Code REST Client and JetBrains allow after a body, returning
// the real body lines and what was dropped.
//
// Without this they are not "ignored" as docs/comparison.md promises: a
// "> {% ... %}" handler is sent as part of the request body, and a leading
// "< {%" is mistaken for the "< ./file" body-file syntax and fails at run time
// looking for a file called "{%". Note "< ./body.json" is a real body file and
// must survive; only "< {%" is a script.
func splitHandlerBlocks(lines []string, nums []int) ([]string, []handlerBlock) {
	var body []string
	var found []handlerBlock
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		num := 0
		if i < len(nums) {
			num = nums[i]
		}
		switch {
		case strings.HasPrefix(t, "> {%"), strings.HasPrefix(t, "< {%"):
			what := "response handler block"
			if strings.HasPrefix(t, "< {%") {
				what = "pre-request script block"
			}
			found = append(found, handlerBlock{line: num, what: what, text: t})
			// Consume to the closing %}, or to the end of the block if the
			// file never closes it.
			for ; i < len(lines); i++ {
				if strings.Contains(lines[i], "%}") {
					break
				}
			}
		case strings.HasPrefix(t, ">> "), strings.HasPrefix(t, ">>! "):
			found = append(found, handlerBlock{line: num, what: "response redirect", text: t})
		case strings.HasPrefix(t, "> ") && !strings.HasPrefix(t, ">> "):
			found = append(found, handlerBlock{line: num, what: "response handler file", text: t})
		default:
			body = append(body, lines[i])
		}
	}
	return body, found
}
