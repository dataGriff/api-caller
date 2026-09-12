// Package curlexport renders a resolved request as a curl command, so a
// request can still be run where apic is not installed.
package curlexport

import (
	"strings"

	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/runner"
)

// Command returns a POSIX-shell curl command for the request.
func Command(r *runner.Resolved) string {
	var parts []string
	parts = append(parts, "curl -sS")
	if (r.Method != "GET" && (r.Method != "POST" || r.Body == "")) || (r.Method == "GET" && r.Body != "") {
		parts = append(parts, "-X "+r.Method)
	}
	for _, h := range r.Headers {
		parts = append(parts, "-H "+quote(h.Name+": "+h.Value))
	}
	if r.Body != "" {
		parts = append(parts, "--data-raw "+quote(r.Body))
	}
	parts = append(parts, authFlags(r.AuthSpec)...)
	parts = append(parts, quote(r.URL))
	return strings.Join(parts, " \\\n  ")
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// authFlags maps an auth spec onto curl's own options where curl has them.
func authFlags(s *auth.Spec) []string {
	if s == nil {
		return nil
	}
	switch s.Type {
	case "bearer":
		return []string{"-H " + quote("Authorization: Bearer "+s.Args[0])}
	case "basic":
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
				`$( [ -n "$AWS_SESSION_TOKEN" ] && printf '%s' "-H x-amz-security-token:$AWS_SESSION_TOKEN" )`,
			}
		}
		return []string{
			"--aws-sigv4 " + quote("aws:amz:"+region+":"+service),
			`--user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY"`,
			`$( [ -n "$AWS_SESSION_TOKEN" ] && printf '%s' "-H x-amz-security-token:$AWS_SESSION_TOKEN" )`,
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
