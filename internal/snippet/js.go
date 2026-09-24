package snippet

import (
	"encoding/json"
	"strings"
)

// js renders fetch for Node 18+ (an ES module, for the top-level await
// and the file reads a multipart body needs) or a browser.
func js(m *request) string {
	var b strings.Builder
	for _, n := range m.notes(false, false, false) {
		b.WriteString("// " + n + "\n")
	}
	hasFile := false
	for _, p := range m.Parts {
		hasFile = hasFile || p.File != ""
	}
	if hasFile {
		b.WriteString("import { readFile } from \"node:fs/promises\";\n\n")
	}
	c := m.Cred
	url := jsQuote(m.URL)
	if c != nil && c.Kind == "query" {
		b.WriteString("const url = new URL(" + jsQuote(m.URL) + ");\n")
		b.WriteString("url.searchParams.append(" + jsQuote(c.Name) + ", " + jsConcat(c.Prefix, c.Value) + ");\n")
		url = "url"
	}
	var headers []string
	for _, h := range m.Headers {
		headers = append(headers, "    "+jsQuote(h.Name)+": "+jsQuote(h.Value)+",")
	}
	if c != nil {
		switch c.Kind {
		case "header":
			headers = append(headers, "    "+jsQuote(c.Name)+": "+jsConcat(c.Prefix, c.Value)+",")
		case "basic":
			headers = append(headers, "    \"Authorization\": \"Basic \" + btoa("+jsSecret(c.User)+" + \":\" + "+jsSecret(c.Pass)+"),")
		}
	}
	if len(m.Parts) > 0 {
		b.WriteString("const form = new FormData();\n")
		for _, p := range m.Parts {
			if p.File == "" {
				v := p.Value
				if m.Redact {
					v = "***"
				}
				b.WriteString("form.append(" + jsQuote(p.Name) + ", " + jsQuote(v) + ");\n")
				continue
			}
			name := p.Filename
			if name == "" {
				name = baseName(p.File)
			}
			opts := ""
			if p.ContentType != "" {
				opts = ", { type: " + jsQuote(p.ContentType) + " }"
			}
			b.WriteString("form.append(" + jsQuote(p.Name) + ", new Blob([await readFile(" + jsQuote(p.File) + ")]" + opts + "), " + jsQuote(name) + ");\n")
		}
	}
	var opts []string
	opts = append(opts, "  method: "+jsQuote(m.Method)+",")
	if len(headers) > 0 {
		opts = append(opts, "  headers: {\n"+strings.Join(headers, "\n")+"\n  },")
	}
	switch {
	case len(m.Parts) > 0:
		opts = append(opts, "  body: form,")
	case m.Body != "":
		opts = append(opts, "  body: "+jsQuote(m.Body)+",")
	}
	b.WriteString("const response = await fetch(" + url + ", {\n" + strings.Join(opts, "\n") + "\n});\n")
	b.WriteString("console.log(response.status);\nconsole.log(await response.text());")
	return b.String()
}

// jsQuote is a JavaScript string literal (JSON's syntax is a subset),
// without JSON's escaping of <, > and &, which a URL is full of.
func jsQuote(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func jsSecret(v secret) string {
	if v.Env != "" {
		return "process.env." + v.Env
	}
	return jsQuote(v.Literal)
}

func jsConcat(prefix string, v secret) string {
	if v.Env != "" {
		if prefix == "" {
			return jsSecret(v)
		}
		return jsQuote(prefix) + " + " + jsSecret(v)
	}
	return jsQuote(prefix + v.Literal)
}
