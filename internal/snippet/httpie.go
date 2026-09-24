package snippet

import (
	"path"
	"strings"
)

// httpie renders an HTTPie command (https://httpie.io/cli), POSIX-shell
// quoted like the curl command.
func httpie(m *request) string {
	var parts []string
	// HTTPie speaks HTTP/1.1 only, so a pinned HTTP/1.1 needs nothing.
	for _, n := range m.notes(true, true, true) {
		parts = append(parts, "# "+n)
	}
	cmd := []string{"http --ignore-stdin"}
	if len(m.Parts) > 0 {
		cmd = append(cmd, "--multipart")
	}
	if t := m.TLS; t != nil {
		switch {
		case t.Insecure:
			cmd = append(cmd, "--verify=no")
		case t.CAFile != "":
			cmd = append(cmd, "--verify="+quote(t.CAFile))
		}
		if t.CertFile != "" {
			cmd = append(cmd, "--cert="+quote(t.CertFile))
			if t.KeyFile != "" && t.KeyFile != t.CertFile {
				cmd = append(cmd, "--cert-key="+quote(t.KeyFile))
			}
		}
	}
	if m.Proxy != "" {
		cmd = append(cmd, "--proxy="+quote("http:"+m.Proxy), "--proxy="+quote("https:"+m.Proxy))
	}
	if c := m.Cred; c != nil {
		switch c.Kind {
		case "basic", "digest":
			if c.Kind == "digest" {
				cmd = append(cmd, "--auth-type=digest")
			}
			cmd = append(cmd, "--auth="+shellPair(c.User, c.Pass))
		}
	}
	if m.Body != "" {
		cmd = append(cmd, "--raw="+quote(m.Body))
	}
	cmd = append(cmd, m.Method, quote(m.URL))
	for _, h := range m.Headers {
		if h.Value == "" {
			cmd = append(cmd, quote(h.Name+";")) // HTTPie's spelling of an empty header
			continue
		}
		cmd = append(cmd, quote(h.Name+":"+h.Value))
	}
	if c := m.Cred; c != nil {
		switch c.Kind {
		case "header":
			cmd = append(cmd, shellItem(c.Name+":"+c.Prefix, c.Value))
		case "query":
			cmd = append(cmd, shellItem(c.Name+"=="+c.Prefix, c.Value))
		}
	}
	for _, p := range m.Parts {
		if p.File == "" {
			v := p.Value
			if m.Redact {
				v = "***"
			}
			cmd = append(cmd, quote(p.Name+"="+v))
			continue
		}
		item := p.Name + "@" + p.File
		if p.ContentType != "" {
			item += ";type=" + p.ContentType
		}
		if p.Filename != "" && p.Filename != path.Base(p.File) {
			item += ";filename=" + p.Filename
		}
		cmd = append(cmd, quote(item))
	}
	parts = append(parts, strings.Join(cmd, " \\\n  "))
	return strings.Join(parts, "\n")
}

// shellItem is text followed by a secret: quoted literally, or with the
// environment variable expanded inside double quotes.
func shellItem(text string, v secret) string {
	if v.Env != "" {
		return `"` + escapeDouble(text) + `$` + v.Env + `"`
	}
	return quote(text + v.Literal)
}

func shellPair(user, pass secret) string {
	if user.Env != "" {
		return `"$` + user.Env + `:$` + pass.Env + `"`
	}
	return quote(user.Literal + ":" + pass.Literal)
}
