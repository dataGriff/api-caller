package snippet

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/dataGriff/api-caller/internal/httpfile"
)

// powershell renders Invoke-RestMethod for PowerShell 7, whose -Form,
// -SkipCertificateCheck and -HttpVersion the snippet uses.
func powershell(m *request) string {
	var b strings.Builder
	// -SkipCertificateCheck covers --insecure; a CA file or client
	// certificate is not something Invoke-RestMethod takes as a path.
	tlsDone := m.TLS == nil || (m.TLS.CAFile == "" && m.TLS.CertFile == "")
	for _, n := range m.notes(tlsDone, true, true) { // -Credential answers digest
		b.WriteString("# " + n + "\n")
	}
	if m.TLS != nil && m.TLS.CertFile != "" {
		b.WriteString("# present the client certificate with -Certificate (Get-PfxCertificate " + psQuote(m.TLS.CertFile) + ")\n")
	}
	uri := psQuote(m.URL)
	if c := m.Cred; c != nil && c.Kind == "query" {
		sep := "?"
		if strings.Contains(m.URL, "?") {
			sep = "&"
		}
		key := psSecret(c.Value)
		if c.Value.Env != "" {
			key = "[uri]::EscapeDataString(" + key + ")"
			uri = psQuote(m.URL+sep+url.QueryEscape(c.Name)+"="+url.QueryEscape(c.Prefix)) + " + " + key
		} else {
			uri = psQuote(m.URL + sep + url.QueryEscape(c.Name) + "=" + url.QueryEscape(c.Prefix+c.Value.Literal))
		}
	}
	contentType := ""
	var headers []string
	for _, h := range m.Headers {
		if strings.EqualFold(h.Name, "Content-Type") {
			contentType = h.Value
			continue
		}
		headers = append(headers, fmt.Sprintf("  %s = %s", psQuote(h.Name), psQuote(h.Value)))
	}
	if c := m.Cred; c != nil {
		switch c.Kind {
		case "header":
			headers = append(headers, fmt.Sprintf("  %s = %s", psQuote(c.Name), psConcat(c.Prefix, c.Value)))
		case "basic":
			pair := psSecret(c.User) + " + ':' + " + psSecret(c.Pass)
			headers = append(headers, fmt.Sprintf("  'Authorization' = 'Basic ' + [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes(%s))", pair))
		}
	}
	args := []string{psMethod(m.Method), "-Uri " + uri}
	if len(headers) > 0 {
		b.WriteString("$headers = @{\n" + strings.Join(headers, "\n") + "\n}\n")
		args = append(args, "-Headers $headers")
	}
	if c := m.Cred; c != nil && c.Kind == "digest" {
		b.WriteString("$credential = [pscredential]::new(" + psSecret(c.User) + ", (ConvertTo-SecureString " + psSecret(c.Pass) + " -AsPlainText -Force))\n")
		args = append(args, "-Credential $credential", "-AllowUnencryptedAuthentication")
	}
	if len(m.Parts) > 0 {
		var form []string
		for _, p := range m.Parts {
			if p.File != "" {
				form = append(form, fmt.Sprintf("  %s = Get-Item %s", psQuote(p.Name), psQuote(p.File)))
				continue
			}
			v := p.Value
			if m.Redact {
				v = "***"
			}
			form = append(form, fmt.Sprintf("  %s = %s", psQuote(p.Name), psQuote(v)))
		}
		b.WriteString("$form = @{\n" + strings.Join(form, "\n") + "\n}\n")
		args = append(args, "-Form $form")
	}
	if m.Body != "" {
		b.WriteString("$body = " + psQuote(m.Body) + "\n")
		args = append(args, "-Body $body")
	}
	if contentType != "" {
		args = append(args, "-ContentType "+psQuote(contentType))
	}
	switch m.HTTP {
	case httpfile.HTTP1:
		args = append(args, "-HttpVersion 1.1")
	case httpfile.HTTP2:
		args = append(args, "-HttpVersion 2.0")
	}
	if m.TLS != nil && m.TLS.Insecure {
		args = append(args, "-SkipCertificateCheck")
	}
	if m.Proxy != "" {
		args = append(args, "-Proxy "+psQuote(m.Proxy))
	}
	b.WriteString("Invoke-RestMethod " + strings.Join(args, " `\n  "))
	return b.String()
}

// psQuote is a single-quoted PowerShell string: nothing inside expands,
// and a quote is doubled.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func psSecret(v secret) string {
	if v.Env != "" {
		return "$env:" + v.Env
	}
	return psQuote(v.Literal)
}

func psConcat(prefix string, v secret) string {
	if v.Env != "" {
		if prefix == "" {
			return "$env:" + v.Env
		}
		return psQuote(prefix) + " + $env:" + v.Env
	}
	return psQuote(prefix + v.Literal)
}

// psMethod is -Method Get, Post, … for the methods PowerShell names, and
// -CustomMethod for any other verb.
func psMethod(m string) string {
	switch m {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE":
		return "-Method " + m[:1] + strings.ToLower(m[1:])
	}
	return "-CustomMethod " + psQuote(m)
}
