// Package curlimport turns a curl command line into a `.http` request
// block, the reverse of `apic curl`: the common flags map onto the request
// line, headers, body and auth; the rest is reported, never fatal.
package curlimport

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// Request is what a curl command line asked for.
type Request struct {
	Method   string
	URL      string
	Headers  []Header
	Body     string // inline body
	BodyFile string // `-d @file`: sent as `< ./file`
	Parts    []Part // -F fields, a multipart body
	User     string // -u user:password, as `# @auth basic`
	Insecure bool   // -k
	Follow   bool   // -L
	Warnings []string
}

// Header is one -H header.
type Header struct{ Name, Value string }

// Part is one -F field.
type Part struct {
	Name        string
	Value       string
	File        string // name=@path
	Filename    string // ;filename=
	ContentType string // ;type=
}

// Parse reads a curl command line, with its shell quoting and line
// continuations, and returns the request it describes. Unknown flags are
// warnings, not errors; an unreadable line or a missing URL is an error.
func Parse(command string) (*Request, error) {
	tokens, err := tokenize(command)
	if err != nil {
		return nil, err
	}
	if len(tokens) > 0 && (tokens[0] == "curl" || strings.HasSuffix(tokens[0], "/curl")) {
		tokens = tokens[1:]
	}
	r := &Request{}
	var data []string
	var dataFile string
	var get, head bool
	var contentTypeSet bool
	warn := func(format string, args ...any) { r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...)) }
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "--" {
			for _, rest := range tokens[i+1:] {
				r.positional(rest, warn)
			}
			break
		}
		if !strings.HasPrefix(tok, "-") || tok == "-" {
			r.positional(tok, warn)
			continue
		}
		name, value, attached := splitFlag(tok)
		spec, known := flags[name]
		if !known {
			// A bundle of boolean short flags: -sSL.
			if !strings.HasPrefix(tok, "--") && len(tok) > 2 && allBoolean(tok[1:]) {
				var single []string
				for _, c := range tok[1:] {
					single = append(single, "-"+string(c))
				}
				tokens = append(tokens[:i], append(single, tokens[i+1:]...)...)
				i--
				continue
			}
			warn("unknown flag %s (ignored)", tok)
			continue
		}
		if spec.takesValue && !attached {
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("%s needs a value", tok)
			}
			i++
			value = tokens[i]
		}
		switch spec.name {
		case "request":
			r.Method = strings.ToUpper(value)
		case "header":
			k, v, ok := strings.Cut(value, ":")
			k = strings.TrimSpace(k)
			if !ok {
				if strings.HasSuffix(k, ";") { // curl's spelling of an empty header
					r.Headers = append(r.Headers, Header{Name: strings.TrimSuffix(k, ";")})
				} else {
					warn("header %q is not `Name: value` (ignored)", value)
				}
				continue
			}
			if strings.EqualFold(k, "Content-Type") {
				contentTypeSet = true
			}
			r.Headers = append(r.Headers, Header{Name: k, Value: strings.TrimSpace(v)})
		case "data", "data-binary", "data-ascii":
			if strings.HasPrefix(value, "@") {
				if dataFile != "" || len(data) > 0 {
					warn("more than one body: %s dropped", value)
					continue
				}
				dataFile = strings.TrimPrefix(value, "@")
				continue
			}
			data = append(data, value)
		case "data-raw":
			data = append(data, value)
		case "data-urlencode":
			data = append(data, urlencodeData(value, warn))
		case "form", "form-string":
			p, ok := parseForm(value, spec.name == "form-string")
			if !ok {
				warn("form field %q is not name=value (ignored)", value)
				continue
			}
			r.Parts = append(r.Parts, p)
		case "user":
			r.User = value
		case "url":
			r.positional(value, warn)
		case "get":
			get = true
		case "head":
			head = true
		case "cookie":
			if !strings.Contains(value, "=") {
				warn("-b %s names a cookie file, which apic does not read; switch the jar on with --cookies instead", value)
				continue
			}
			r.Headers = append(r.Headers, Header{Name: "Cookie", Value: value})
		case "user-agent":
			r.Headers = append(r.Headers, Header{Name: "User-Agent", Value: value})
		case "referer":
			r.Headers = append(r.Headers, Header{Name: "Referer", Value: value})
		case "insecure":
			r.Insecure = true
		case "location":
			r.Follow = true
		case "compressed":
			// apic decompresses gzip responses on its own.
		case "quiet":
		case "tls":
			warn("%s: configure tls: in apic.yaml, or pass %s to apic itself", tok, tok)
		case "ignored":
			warn("%s is about curl's own output or transport and has no place in a request file (ignored)", tok)
		}
	}
	if r.URL == "" {
		return nil, errors.New("no URL in the command")
	}
	body := strings.Join(data, "&")
	switch {
	case get && (body != "" || dataFile != ""):
		if dataFile != "" {
			warn("-G with a body file: the file is not appended to the URL")
		}
		if body != "" {
			sep := "?"
			if strings.Contains(r.URL, "?") {
				sep = "&"
			}
			r.URL += sep + body
		}
	case dataFile != "":
		r.BodyFile = dataFile
	default:
		r.Body = body
	}
	hasBody := r.Body != "" || r.BodyFile != "" || len(r.Parts) > 0
	if r.Method == "" {
		switch {
		case head:
			r.Method = "HEAD"
		case hasBody && !get:
			r.Method = "POST"
		default:
			r.Method = "GET"
		}
	}
	if len(r.Parts) > 0 {
		r.Headers = withoutContentType(r.Headers)
		r.Headers = append(r.Headers, Header{Name: "Content-Type", Value: "multipart/form-data; boundary=WebAppBoundary"})
	} else if (r.Body != "" || r.BodyFile != "") && !contentTypeSet {
		// curl's default for -d.
		r.Headers = append(r.Headers, Header{Name: "Content-Type", Value: "application/x-www-form-urlencoded"})
	}
	return r, nil
}

func (r *Request) positional(tok string, warn func(string, ...any)) {
	if r.URL != "" {
		warn("extra argument %q (ignored)", tok)
		return
	}
	if !strings.Contains(tok, "://") {
		tok = "http://" + tok // curl's own default
	}
	r.URL = tok
}

type flagSpec struct {
	name       string
	takesValue bool
}

// flags maps every curl option apic reads (or knowingly ignores) to what
// it means. Long options are keyed with their dashes, short ones too.
var flags = map[string]flagSpec{
	"-X": {"request", true}, "--request": {"request", true},
	"-H": {"header", true}, "--header": {"header", true},
	"-d": {"data", true}, "--data": {"data", true}, "--data-ascii": {"data-ascii", true},
	"--data-raw": {"data-raw", true}, "--data-binary": {"data-binary", true}, "--data-urlencode": {"data-urlencode", true},
	"-F": {"form", true}, "--form": {"form", true}, "--form-string": {"form-string", true},
	"-u": {"user", true}, "--user": {"user", true},
	"--url": {"url", true},
	"-G":    {"get", false}, "--get": {"get", false},
	"-I": {"head", false}, "--head": {"head", false},
	"-b": {"cookie", true}, "--cookie": {"cookie", true},
	"-A": {"user-agent", true}, "--user-agent": {"user-agent", true},
	"-e": {"referer", true}, "--referer": {"referer", true},
	"-k": {"insecure", false}, "--insecure": {"insecure", false},
	"-L": {"location", false}, "--location": {"location", false},
	"--compressed": {"compressed", false},
	"-s":           {"quiet", false}, "--silent": {"quiet", false}, "-S": {"quiet", false}, "--show-error": {"quiet", false},
	"-v": {"quiet", false}, "--verbose": {"quiet", false}, "-i": {"quiet", false}, "--include": {"quiet", false},
	"-#": {"quiet", false}, "--progress-bar": {"quiet", false}, "-f": {"quiet", false}, "--fail": {"quiet", false},
	"--cacert": {"tls", true}, "--cert": {"tls", true}, "--key": {"tls", true}, "-E": {"tls", true},
	"-o": {"ignored", true}, "--output": {"ignored", true}, "-w": {"ignored", true}, "--write-out": {"ignored", true},
	"-x": {"ignored", true}, "--proxy": {"ignored", true}, "--max-time": {"ignored", true}, "-m": {"ignored", true},
	"--connect-timeout": {"ignored", true}, "--retry": {"ignored", true}, "-c": {"ignored", true}, "--cookie-jar": {"ignored", true},
	"-D": {"ignored", true}, "--dump-header": {"ignored", true}, "-O": {"ignored", false}, "--remote-name": {"ignored", false},
}

// splitFlag separates `--name=value` and `-Xvalue` forms.
func splitFlag(tok string) (name, value string, attached bool) {
	if strings.HasPrefix(tok, "--") {
		if k, v, ok := strings.Cut(tok, "="); ok {
			return k, v, true
		}
		return tok, "", false
	}
	if len(tok) > 2 {
		short := tok[:2]
		if spec, ok := flags[short]; ok && spec.takesValue {
			return short, tok[2:], true
		}
	}
	return tok, "", false
}

func allBoolean(chars string) bool {
	for _, c := range chars {
		spec, ok := flags["-"+string(c)]
		if !ok || spec.takesValue {
			return false
		}
	}
	return true
}

// urlencodeData handles --data-urlencode's four spellings: content,
// =content, name=content and name@file.
func urlencodeData(v string, warn func(string, ...any)) string {
	if name, file, ok := strings.Cut(v, "@"); ok && !strings.Contains(name, "=") {
		warn("--data-urlencode %s reads a file, which apic does not do here; paste its content", v)
		return name + "=" + url.QueryEscape("@"+file)
	}
	if strings.HasPrefix(v, "=") {
		return escape(v[1:])
	}
	if name, content, ok := strings.Cut(v, "="); ok {
		return name + "=" + escape(content)
	}
	return escape(v)
}

// escape percent-encodes a value, keeping {{placeholders}} for the runner.
func escape(s string) string {
	return strings.NewReplacer("%7B%7B", "{{", "%7D%7D", "}}").Replace(url.QueryEscape(s))
}

// parseForm reads a -F field: name=value, name=@file[;type=..][;filename=..]
// or name=<file (the content of a file as text).
func parseForm(v string, literal bool) (Part, bool) {
	name, rest, ok := strings.Cut(v, "=")
	if !ok || name == "" {
		return Part{}, false
	}
	p := Part{Name: name}
	if literal || (!strings.HasPrefix(rest, "@") && !strings.HasPrefix(rest, "<")) {
		p.Value = rest
		return p, true
	}
	spec := strings.Split(rest, ";")
	p.File = strings.TrimLeft(spec[0], "@<")
	for _, opt := range spec[1:] {
		k, val, _ := strings.Cut(opt, "=")
		switch strings.TrimSpace(k) {
		case "type":
			p.ContentType = strings.TrimSpace(val)
		case "filename":
			p.Filename = strings.TrimSpace(val)
		}
	}
	if strings.HasPrefix(rest, "<") {
		// `<file` sends the file's content as a text field: same thing
		// from a request file's point of view, without a filename.
		p.Filename = ""
	} else if p.Filename == "" {
		p.Filename = path.Base(strings.ReplaceAll(p.File, "\\", "/"))
	}
	return p, true
}

func withoutContentType(hs []Header) []Header {
	out := hs[:0:0]
	for _, h := range hs {
		if !strings.EqualFold(h.Name, "Content-Type") {
			out = append(out, h)
		}
	}
	return out
}

// tokenize splits a shell command line: whitespace separates words,
// single quotes are literal, double quotes keep spaces and take backslash
// escapes, $'...' takes C escapes, and a backslash before a newline joins
// lines.
func tokenize(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			if i+1 >= len(s) {
				return nil, errors.New("trailing backslash")
			}
			i++
			if s[i] == '\n' {
				continue // line continuation
			}
			if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i++
				continue
			}
			cur.WriteByte(s[i])
			inWord = true
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, errors.New("unterminated single quote")
			}
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case c == '"':
			i++
			closed := false
			for ; i < len(s); i++ {
				if s[i] == '"' {
					closed = true
					break
				}
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`\n", s[i+1]) >= 0 {
					i++
					if s[i] == '\n' {
						continue
					}
				}
				cur.WriteByte(s[i])
			}
			if !closed {
				return nil, errors.New("unterminated double quote")
			}
			inWord = true
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			i += 2
			closed := false
			for ; i < len(s); i++ {
				if s[i] == '\'' {
					closed = true
					break
				}
				if s[i] == '\\' && i+1 < len(s) {
					i++
					switch s[i] {
					case 'n':
						cur.WriteByte('\n')
					case 't':
						cur.WriteByte('\t')
					case 'r':
						cur.WriteByte('\r')
					default:
						cur.WriteByte(s[i])
					}
					continue
				}
				cur.WriteByte(s[i])
			}
			if !closed {
				return nil, errors.New("unterminated $'...' quote")
			}
			inWord = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out, nil
}

var reNonWord = regexp.MustCompile(`[^A-Za-z0-9]+`)

// Name derives a request name from the method and path when --name is not
// given: `POST /todos/42` becomes post-todos-42.
func Name(r *Request) string {
	u, err := url.Parse(r.URL)
	p := ""
	if err == nil {
		p = u.Path
	}
	name := strings.Trim(strings.ToLower(reNonWord.ReplaceAllString(r.Method+" "+p, "-")), "-")
	if name == "" {
		return "request"
	}
	return name
}

// Render writes the request as a `###` block. baseURL, when it prefixes
// the URL, is replaced by {{baseUrl}}.
func Render(r *Request, name, baseURL string) string {
	var b strings.Builder
	rawURL := r.URL
	if baseURL != "" {
		base := strings.TrimRight(baseURL, "/")
		if strings.HasPrefix(rawURL, base+"/") || rawURL == base || strings.HasPrefix(rawURL, base+"?") {
			rawURL = "{{baseUrl}}" + strings.TrimPrefix(rawURL, base)
		}
	}
	fmt.Fprintf(&b, "### %s %s\n", r.Method, urlPath(rawURL))
	fmt.Fprintf(&b, "# @name %s\n", name)
	if r.User != "" {
		u, p, _ := strings.Cut(r.User, ":")
		fmt.Fprintf(&b, "# @auth basic %s %s\n", authArg(u), authArg(p))
	}
	if r.Insecure {
		b.WriteString("# curl was run with --insecure; run this with `apic --insecure` or set tls.verifyHost in apic.yaml\n")
	}
	b.WriteString("# @assert status == 200\n")
	fmt.Fprintf(&b, "%s %s\n", r.Method, oneLine(rawURL))
	for _, h := range r.Headers {
		fmt.Fprintf(&b, "%s: %s\n", oneLine(h.Name), oneLine(h.Value))
	}
	switch {
	case len(r.Parts) > 0:
		b.WriteString("\n")
		for _, p := range r.Parts {
			b.WriteString("--WebAppBoundary\n")
			if p.File != "" {
				if p.Filename != "" {
					fmt.Fprintf(&b, "Content-Disposition: form-data; name=%q; filename=%q\n", p.Name, p.Filename)
				} else {
					fmt.Fprintf(&b, "Content-Disposition: form-data; name=%q\n", p.Name)
				}
				if p.ContentType != "" {
					fmt.Fprintf(&b, "Content-Type: %s\n", oneLine(p.ContentType))
				}
				fmt.Fprintf(&b, "\n< %s\n", relativeFile(p.File))
				continue
			}
			fmt.Fprintf(&b, "Content-Disposition: form-data; name=%q\n", p.Name)
			if p.ContentType != "" {
				fmt.Fprintf(&b, "Content-Type: %s\n", oneLine(p.ContentType))
			}
			fmt.Fprintf(&b, "\n%s\n", p.Value)
		}
		b.WriteString("--WebAppBoundary--\n")
	case r.BodyFile != "":
		fmt.Fprintf(&b, "\n< %s\n", relativeFile(r.BodyFile))
	case r.Body != "":
		fmt.Fprintf(&b, "\n%s\n", r.Body)
	}
	return b.String()
}

// relativeFile keeps a `< file` reference relative to the .http file, the
// only place the runner reads it from.
func relativeFile(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return p
	}
	return "./" + p
}

func urlPath(rawURL string) string {
	u, err := url.Parse(strings.ReplaceAll(rawURL, "{{baseUrl}}", "http://baseurl"))
	if err != nil || u.Path == "" {
		return oneLine(rawURL)
	}
	return u.Path
}

func authArg(s string) string {
	s = oneLine(s)
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
