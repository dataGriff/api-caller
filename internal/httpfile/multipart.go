package httpfile

import (
	"errors"
	"fmt"
	"mime"
	"strings"
)

// Part is one part of a multipart/form-data body, as written in the file:
// its headers, then either literal content or a `< file` reference.
type Part struct {
	Headers     []Header
	Name        string // from Content-Disposition
	Filename    string // from Content-Disposition, when the part is a file
	ContentType string // the part's own Content-Type header, when present
	Body        string // literal content (a template), empty when File is set
	File        string // `< path`: the file whose bytes are the content
	// FileTemplated is `<@ path`: substitute {{vars}} inside the file too.
	FileTemplated bool
	FileLine      int // line of the `< path` reference
	FileColumn    int // column where the path starts on that line
	Line          int // line of the delimiter that opens the part
}

// Multipart is a parsed multipart/form-data body.
type Multipart struct {
	Boundary string
	Parts    []Part
}

// Files counts the parts whose content comes from a file.
func (m *Multipart) Files() int {
	n := 0
	for _, p := range m.Parts {
		if p.File != "" {
			n++
		}
	}
	return n
}

// Header returns the value of the first header with the given name,
// case-insensitively.
func (r *Request) Header(name string) (string, bool) {
	for _, h := range r.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value, true
		}
	}
	return "", false
}

// IsMultipart reports whether the request declares a multipart/form-data
// Content-Type. The body then holds parts (see Multipart) rather than one
// template.
func (r *Request) IsMultipart() bool {
	ct, ok := r.Header("Content-Type")
	if !ok {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && strings.EqualFold(mt, "multipart/form-data")
}

// Multipart parses the body of a multipart/form-data request into its
// parts. It returns nil, nil for a request that is not multipart, or whose
// whole body is a `< file`; an error names what is wrong with the body.
func (r *Request) Multipart() (*Multipart, error) {
	if !r.IsMultipart() || r.BodyFile != "" {
		return nil, nil
	}
	ct, _ := r.Header("Content-Type")
	_, params, err := mime.ParseMediaType(ct)
	if err != nil {
		return nil, fmt.Errorf("Content-Type %q: %w", ct, err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("Content-Type multipart/form-data needs a boundary parameter, e.g. `multipart/form-data; boundary=WebAppBoundary`")
	}
	if strings.TrimSpace(r.Body) == "" {
		return nil, errors.New("multipart body is empty: write the parts between `--" + boundary + "` delimiters")
	}
	delim, closing := "--"+boundary, "--"+boundary+"--"
	lines := strings.Split(r.Body, "\n")
	m := &Multipart{Boundary: boundary}
	i := 0
	for i < len(lines) && strings.TrimRight(lines[i], " \t") != delim {
		if strings.TrimRight(lines[i], " \t") == closing {
			break
		}
		i++
	}
	if i == len(lines) {
		return nil, fmt.Errorf("multipart body has no `%s` delimiter", delim)
	}
	closed := false
	for i < len(lines) {
		t := strings.TrimRight(lines[i], " \t")
		if t == closing {
			closed = true
			break
		}
		if t != delim {
			return nil, fmt.Errorf("multipart body: line %d: expected `%s`", r.BodyLine+i, delim)
		}
		part := Part{Line: r.BodyLine + i}
		i++
		for i < len(lines) {
			t := strings.TrimSpace(lines[i])
			if t == "" {
				i++
				break
			}
			mh := reHeader.FindStringSubmatch(t)
			if mh == nil {
				return nil, fmt.Errorf("multipart body: line %d: expected a part header (`Name: value`) or a blank line before the content, got %q", r.BodyLine+i, t)
			}
			part.Headers = append(part.Headers, Header{Name: mh[1], Value: mh[2]})
			i++
		}
		start := i
		for i < len(lines) {
			t := strings.TrimRight(lines[i], " \t")
			if t == delim || t == closing {
				break
			}
			i++
		}
		if i == len(lines) {
			return nil, fmt.Errorf("multipart body is not closed: expected `%s` at the end", closing)
		}
		content := lines[start:i]
		if len(content) == 1 {
			if t := strings.TrimSpace(content[0]); strings.HasPrefix(t, "<@ ") || strings.HasPrefix(t, "< ") {
				part.FileTemplated = strings.HasPrefix(t, "<@")
				part.File = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t, "<@"), "<"))
				part.FileLine = r.BodyLine + start
				part.FileColumn, _ = Span(content[0], part.File, strings.Index(content[0], "<"))
			}
		}
		if part.File == "" {
			part.Body = strings.Join(content, "\n")
		}
		for _, h := range part.Headers {
			switch {
			case strings.EqualFold(h.Name, "Content-Disposition"):
				if _, p, err := mime.ParseMediaType(h.Value); err == nil {
					part.Name, part.Filename = p["name"], p["filename"]
				}
			case strings.EqualFold(h.Name, "Content-Type"):
				part.ContentType = h.Value
			}
		}
		m.Parts = append(m.Parts, part)
	}
	if !closed {
		return nil, fmt.Errorf("multipart body is not closed: expected `%s` at the end", closing)
	}
	if len(m.Parts) == 0 {
		return nil, fmt.Errorf("multipart body has no parts between `%s` and `%s`", delim, closing)
	}
	return m, nil
}

// QuoteParam quotes a Content-Disposition parameter value the way RFC 2616
// quoted-strings are written: only backslash and the double quote are
// escaped, so a non-ASCII name or filename survives mime.ParseMediaType
// (Go's %q would write \u escapes it reads as literal letters).
func QuoteParam(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// FileRef is a `< path` or `<@ path` reference in a request: the whole
// body, or one part of a multipart body.
type FileRef struct {
	Path      string
	Templated bool
	Line      int
	Column    int
}

// BodyFiles returns every file the request's body reads: the whole-body
// `< file`, or the `< file` parts of a multipart body. A multipart body that
// does not parse contributes nothing; validate reports it.
func (r *Request) BodyFiles() []FileRef {
	if r.BodyFile != "" {
		return []FileRef{{Path: r.BodyFile, Templated: r.BodyFileTemplated, Line: r.BodyFileLine, Column: r.BodyFileColumn}}
	}
	m, err := r.Multipart()
	if err != nil || m == nil {
		return nil
	}
	var out []FileRef
	for _, p := range m.Parts {
		if p.File != "" {
			out = append(out, FileRef{Path: p.File, Templated: p.FileTemplated, Line: p.FileLine, Column: p.FileColumn})
		}
	}
	return out
}
