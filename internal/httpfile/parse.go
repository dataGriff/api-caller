package httpfile

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Diagnostic is a non-fatal problem found while parsing.
type Diagnostic struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Severity string `json:"severity"` // "error" or "warning"
	Message  string `json:"message"`
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", d.Path, d.Line, d.Severity, d.Message)
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
	reHeader      = regexp.MustCompile(`^([\w-]+):\s*(.*)$`)
	reCapture     = regexp.MustCompile(`^([A-Za-z_][\w.-]*)\s*=\s*(.+)$`)
)

// ParseFile reads and parses a .http file from disk.
func ParseFile(path string) (*File, []Diagnostic, error) {
	data, err := os.ReadFile(path)
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

func (p *parser) warn(line int, format string, args ...any) {
	p.diags = append(p.diags, Diagnostic{Path: p.file.Path, Line: line, Severity: "warning", Message: fmt.Sprintf(format, args...)})
}

func (p *parser) errorf(line int, format string, args ...any) {
	p.diags = append(p.diags, Diagnostic{Path: p.file.Path, Line: line, Severity: "error", Message: fmt.Sprintf(format, args...)})
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
			p.parseComment(req, strings.TrimSpace(m[1]), n)
			continue
		}
		break
	}
	if i >= len(b.lines) {
		if len(req.Directives) > 0 {
			p.errorf(b.start, "directives without a request line")
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
		p.errorf(b.nums[i], "expected a header (`Name: value`) or a blank line before the body, got %q", t)
	}

	// Body: the rest of the block, trailing blank lines trimmed.
	if i < len(b.lines) {
		body := strings.Join(b.lines[i:], "\n")
		body = strings.TrimRight(body, "\n\t ")
		body = strings.TrimLeft(body, "\n")
		if t := strings.TrimSpace(body); strings.HasPrefix(t, "<@ ") || strings.HasPrefix(t, "< ") {
			req.BodyFileTemplated = strings.HasPrefix(t, "<@")
			req.BodyFile = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t, "<@"), "<"))
		} else {
			req.Body = body
		}
	}
	if req.Description == "" {
		req.Description = req.Title
	}
	return req
}

func (p *parser) parseComment(req *Request, text string, line int) {
	m := reDirective.FindStringSubmatch(text)
	if m == nil {
		return // plain comment
	}
	key, value := m[1], strings.TrimSpace(m[2])
	req.Directives = append(req.Directives, Directive{Key: key, Value: value, Line: line})
	switch key {
	case "name":
		if value == "" {
			p.errorf(line, "@name needs a value")
		}
		req.Name = value
	case "description":
		req.Description = value
	case "capture":
		cm := reCapture.FindStringSubmatch(value)
		if cm == nil {
			p.errorf(line, "@capture must look like `name = selector`, got %q", value)
			return
		}
		req.Captures = append(req.Captures, Capture{Name: cm[1], Selector: strings.TrimSpace(cm[2]), Line: line})
	case "assert":
		if value == "" {
			p.errorf(line, "@assert needs an expression")
			return
		}
		req.Asserts = append(req.Asserts, Assert{Expr: value, Line: line})
	default:
		if _, ok := KnownDirectives[key]; !ok {
			p.warn(line, "unknown directive @%s (ignored)", key)
		}
	}
}
