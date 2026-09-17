// Package curlexport renders a resolved request as a curl command, so a
// request can still be run where apic is not installed.
package curlexport

import (
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
		parts = append(parts, "-H "+quote(h.Name+": "+h.Value))
	}
	if body != "" {
		parts = append(parts, "--data-raw "+quote(body))
	}
	parts = append(parts, authFlags(r.AuthSpec, redact)...)
	parts = append(parts, quote(r.DisplayURL(redact)))
	return strings.Join(parts, " \\\n  ")
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
