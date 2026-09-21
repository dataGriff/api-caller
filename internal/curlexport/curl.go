// Package curlexport renders a resolved request as a curl command, so a
// request can still be run where apic is not installed.
package curlexport

import (
	"path"
	"strings"

	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/runner"
)

// Command returns a POSIX-shell curl command for the request. With redact set
// the header values, body and query values are masked and the bearer and basic
// credentials become shell placeholders, so the command can go into a stored
// log; without it the command is runnable as printed.
func Command(r *runner.Resolved, redact bool) string {
	var parts []string
	parts = append(parts, "curl -sS")
	body := r.DisplayBody(redact)
	if (r.Method != "GET" && (r.Method != "POST" || body == "")) || (r.Method == "GET" && body != "") {
		parts = append(parts, "-X "+r.Method)
	}
	// Without redact the command must stay runnable as printed, so headers go
	// out verbatim — including Authorization, which DisplayHeaders masks
	// unconditionally for ordinary output. Redacting swaps in that masking.
	headers := r.Headers
	if redact {
		headers = r.DisplayHeaders(true)
	}
	for _, h := range headers {
		if len(r.Parts) > 0 && strings.EqualFold(h.Name, "Content-Type") {
			continue // -F sets multipart/form-data with curl's own boundary
		}
		parts = append(parts, "-H "+quote(h.Name+": "+h.Value))
	}
	switch {
	case len(r.Parts) > 0:
		parts = append(parts, formFlags(r.Parts, redact)...)
	case body != "":
		parts = append(parts, "--data-raw "+quote(body))
	}
	parts = append(parts, authFlags(r.AuthSpec, redact)...)
	parts = append(parts, tlsFlags(r.TLS)...)
	parts = append(parts, proxyFlags(r.Proxy, redact)...)
	parts = append(parts, quote(r.DisplayURL(redact)))
	return strings.Join(parts, " \\\n  ")
}

// formFlags maps the parts of a multipart body onto curl's form options: a
// text part as --form-string name=value (which, unlike -F, never reads a
// value starting with @ or < as a file), a file part as -F name=@path with
// curl's own ;type= and ;filename= suffixes where the file declares them.
// Under redact the text values are masked; a path is not a secret.
func formFlags(parts []runner.FormPart, redact bool) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.File == "" {
			v := p.Value
			if redact {
				v = runner.Masked
			}
			out = append(out, "--form-string "+quote(p.Name+"="+v))
			continue
		}
		spec := p.Name + "=@" + p.File
		if p.ContentType != "" {
			spec += ";type=" + p.ContentType
		}
		if p.Filename != "" && p.Filename != path.Base(p.File) {
			spec += ";filename=" + p.Filename
		}
		out = append(out, "-F "+quote(spec))
	}
	return out
}

// tlsFlags maps the request's TLS setup onto curl's own options.
func tlsFlags(t *runner.TLSInfo) []string {
	if t == nil {
		return nil
	}
	var out []string
	if t.CAFile != "" {
		out = append(out, "--cacert "+quote(t.CAFile))
	}
	if t.CertFile != "" {
		out = append(out, "--cert "+quote(t.CertFile))
		if t.KeyFile != "" && t.KeyFile != t.CertFile {
			out = append(out, "--key "+quote(t.KeyFile))
		}
	}
	if t.Insecure {
		out = append(out, "--insecure")
	}
	return out
}

// proxyFlags maps the proxy in effect onto curl: --noproxy '*' when apic
// would send directly although one is configured, --proxy otherwise. Under
// redact the proxy's own credentials are masked with the rest.
func proxyFlags(p *runner.ProxyInfo, redact bool) []string {
	switch {
	case p == nil:
		return nil
	case p.Off:
		return []string{"--noproxy '*'"}
	case redact || p.Raw() == nil:
		return []string{"--proxy " + quote(p.URL)}
	default:
		return []string{"--proxy " + quote(p.Raw().String())}
	}
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// authFlags maps an auth spec onto curl's own options where curl has them.
// The aws, oauth2 and exec branches never embed a credential; under redact
// bearer and basic join them, so every branch is placeholder-only.
func authFlags(s *auth.Spec, redact bool) []string {
	if s == nil {
		return nil
	}
	switch s.Type {
	case "bearer":
		if redact {
			return []string{`-H "Authorization: Bearer $TOKEN"`}
		}
		return []string{"-H " + quote("Authorization: Bearer "+s.Args[0])}
	case "basic":
		if redact {
			return []string{`--user "$APIC_USER:$APIC_PASSWORD"`}
		}
		return []string{"--user " + quote(s.Args[0]+":"+s.Args[1])}
	case "aws":
		service := s.Options["service"]
		if service == "" {
			service = "execute-api"
		}
		region := s.Options["region"]
		if region == "" {
			return []string{
				`--aws-sigv4 "aws:amz:$AWS_REGION:` + escapeDouble(service) + `"`,
				`--user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY"`,
				`${AWS_SESSION_TOKEN:+-H}`,
				`${AWS_SESSION_TOKEN:+x-amz-security-token:$AWS_SESSION_TOKEN}`,
			}
		}
		return []string{
			"--aws-sigv4 " + quote("aws:amz:"+region+":"+service),
			`--user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY"`,
			`${AWS_SESSION_TOKEN:+-H}`,
			`${AWS_SESSION_TOKEN:+x-amz-security-token:$AWS_SESSION_TOKEN}`,
		}
	case "oauth2":
		return []string{`-H "Authorization: Bearer $TOKEN"`}
	case "exec":
		header := s.Options["header"]
		if header == "" {
			header = "Authorization"
		}
		prefix, ok := s.Options["prefix"]
		if !ok {
			prefix = "Bearer"
		}
		if prefix != "" {
			prefix += " "
		}
		args := make([]string, 0, len(s.Args))
		for _, arg := range s.Args {
			args = append(args, quote(arg))
		}
		return []string{"-H " + quote(header+": "+prefix) + "$(" + strings.Join(args, " ") + ")"}
	}
	return nil
}

func escapeDouble(s string) string {
	repl := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", "\\$")
	return repl.Replace(s)
}
