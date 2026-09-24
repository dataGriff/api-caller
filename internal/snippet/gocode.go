package snippet

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// goCode renders a Go program using net/http, written as gofmt would
// write it (a test holds it to that; go/format itself would add most of
// a megabyte to the binary for this one use).
func goCode(m *request) (string, error) {
	imports := map[string]bool{"fmt": true, "io": true, "net/http": true}
	var body strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&body, format+"\n", args...) }
	c := m.Cred

	switch {
	case len(m.Parts) > 0:
		imports["bytes"], imports["mime/multipart"] = true, true
		w("var buf bytes.Buffer")
		w("form := multipart.NewWriter(&buf)")
		for _, p := range m.Parts {
			if p.File == "" {
				v := p.Value
				if m.Redact {
					v = "***"
				}
				w("if err := form.WriteField(%s, %s); err != nil {\npanic(err)\n}", goQuote(p.Name), goQuote(v))
				continue
			}
			imports["os"], imports["net/textproto"] = true, true
			name := p.Filename
			if name == "" {
				name = baseName(p.File)
			}
			ct := p.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			w("{")
			w("data, err := os.ReadFile(%s)\nif err != nil {\npanic(err)\n}", goQuote(p.File))
			w("h := textproto.MIMEHeader{}")
			w("h.Set(\"Content-Disposition\", %s)", goQuote(fmt.Sprintf("form-data; name=%q; filename=%q", p.Name, name)))
			w("h.Set(\"Content-Type\", %s)", goQuote(ct))
			w("part, err := form.CreatePart(h)\nif err != nil {\npanic(err)\n}")
			w("if _, err := part.Write(data); err != nil {\npanic(err)\n}")
			w("}")
		}
		w("if err := form.Close(); err != nil {\npanic(err)\n}")
		w("req, err := http.NewRequest(%s, %s, &buf)", goQuote(m.Method), goQuote(m.URL))
	case m.Body != "":
		imports["strings"] = true
		w("req, err := http.NewRequest(%s, %s, strings.NewReader(%s))", goQuote(m.Method), goQuote(m.URL), goQuote(m.Body))
	default:
		w("req, err := http.NewRequest(%s, %s, nil)", goQuote(m.Method), goQuote(m.URL))
	}
	w("if err != nil {\npanic(err)\n}")
	for _, h := range m.Headers {
		if strings.EqualFold(h.Name, "Host") {
			w("req.Host = %s", goQuote(h.Value))
			continue
		}
		w("req.Header.Add(%s, %s)", goQuote(h.Name), goQuote(h.Value))
	}
	if len(m.Parts) > 0 {
		w("req.Header.Set(\"Content-Type\", form.FormDataContentType())")
	}
	if c != nil {
		if c.Value.Env != "" || c.User.Env != "" {
			imports["os"] = true
		}
		switch c.Kind {
		case "header":
			w("req.Header.Set(%s, %s)", goQuote(c.Name), goConcat(c.Prefix, c.Value))
		case "query":
			w("q := req.URL.Query()\nq.Add(%s, %s)\nreq.URL.RawQuery = q.Encode()", goQuote(c.Name), goConcat(c.Prefix, c.Value))
		case "basic":
			w("req.SetBasicAuth(%s, %s)", goSecret(c.User), goSecret(c.Pass))
		}
	}
	client := "http.DefaultClient"
	t := m.TLS
	if (t != nil && t.Insecure) || m.Proxy != "" {
		client = "client"
		w("transport := http.DefaultTransport.(*http.Transport).Clone()")
		if t != nil && t.Insecure {
			imports["crypto/tls"] = true
			w("transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // as apic --insecure")
		}
		if m.Proxy != "" {
			imports["net/url"] = true
			w("proxy, err := url.Parse(%s)\nif err != nil {\npanic(err)\n}", goQuote(m.Proxy))
			w("transport.Proxy = http.ProxyURL(proxy)")
		}
		w("client := &http.Client{Transport: transport}")
	}
	w("resp, err := %s.Do(req)\nif err != nil {\npanic(err)\n}", client)
	w("defer resp.Body.Close()")
	w("data, err := io.ReadAll(resp.Body)\nif err != nil {\npanic(err)\n}")
	w("fmt.Println(resp.Status)\nfmt.Println(string(data))")

	var src strings.Builder
	// Only --insecure and the proxy are carried over; a CA or client
	// certificate would need a tls.Config built from the files.
	tlsDone := t == nil || (t.CAFile == "" && t.CertFile == "")
	for _, n := range m.notes(tlsDone, true, false) {
		src.WriteString("// " + n + "\n")
	}
	src.WriteString("package main\n\nimport (\n")
	names := make([]string, 0, len(imports))
	for name := range imports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		src.WriteString("\t" + strconv.Quote(name) + "\n")
	}
	src.WriteString(")\n\nfunc main() {\n" + indent(body.String()) + "}")
	return src.String(), nil
}

// indent puts each line of a function body at its brace depth, one tab
// per level inside func main.
func indent(body string) string {
	var b strings.Builder
	depth := 1
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if strings.HasPrefix(line, "}") {
			depth--
		}
		b.WriteString(strings.Repeat("\t", depth) + line + "\n")
		if strings.HasSuffix(line, "{") {
			depth++
		}
	}
	return b.String()
}

// goQuote is a Go string literal.
func goQuote(s string) string { return strconv.Quote(s) }

func goSecret(v secret) string {
	if v.Env != "" {
		return "os.Getenv(" + goQuote(v.Env) + ")"
	}
	return goQuote(v.Literal)
}

func goConcat(prefix string, v secret) string {
	if v.Env != "" {
		if prefix == "" {
			return goSecret(v)
		}
		return goQuote(prefix) + "+" + goSecret(v) // gofmt's spacing inside a call
	}
	return goQuote(prefix + v.Literal)
}
