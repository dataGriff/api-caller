// Package httpfile parses the common subset of the `.http` request file format
// used by VS Code REST Client, JetBrains HTTP Client, kulala.nvim and httpyac,
// plus apic's own `# @capture` / `# @assert` comment directives.
package httpfile

// File is a parsed .http file.
type File struct {
	Path     string
	Vars     []Var
	Requests []*Request
}

// Var is a file-level variable declared as `@name = value`.
type Var struct {
	Name  string
	Value string
	Line  int
}

// Directive is a `# @key value` comment placed before a request line.
type Directive struct {
	Key   string
	Value string
	Line  int
}

// Capture declares that a value selected from the response should be stored
// under Name for later requests: `# @capture token = body.$.access_token`.
type Capture struct {
	Name     string
	Selector string
	Line     int
}

// Assert is a raw assertion expression: `# @assert status == 200`.
type Assert struct {
	Expr string
	Line int
}

// Header is a raw, un-templated request header.
type Header struct {
	Name  string
	Value string
}

// Request is one request block within a file.
type Request struct {
	File              *File
	Index             int    // 1-based position within the file
	Name              string // from `# @name`, may be empty
	Title             string // free text after `###`
	Description       string // from `# @description`, else Title
	Directives        []Directive
	Captures          []Capture
	Asserts           []Assert
	Method            string
	URL               string // raw template
	HTTPVersion       string
	Headers           []Header
	Body              string // raw template, empty when none
	BodyFile          string // set when the body is `< ./file`
	BodyFileTemplated bool   // `<@ ./file`: substitute {{vars}} inside the file too
	Line              int    // line number of the request line
}

// ID returns the stable identifier used on the command line: the request's
// name when it has one, else `<file>#<index>`.
func (r *Request) ID() string {
	if r.Name != "" {
		return r.Name
	}
	return r.File.Path + "#" + itoa(r.Index)
}

// Steps returns every `# @step` phrase declared on the request.
func (r *Request) Steps() []string {
	var out []string
	for _, d := range r.Directives {
		if d.Key == "step" {
			out = append(out, d.Value)
		}
	}
	return out
}

// Directive returns the value of the first directive with the given key.
func (r *Request) Directive(key string) (string, bool) {
	for _, d := range r.Directives {
		if d.Key == key {
			return d.Value, true
		}
	}
	return "", false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
