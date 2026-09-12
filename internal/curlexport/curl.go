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
			region = "$AWS_REGION"
		}
		return []string{
			"--aws-sigv4 " + quote("aws:amz:"+region+":"+service),
			`--user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY"`,
			`-H "x-amz-security-token: $AWS_SESSION_TOKEN"`,
		}
	case "oauth2":
		return []string{`-H "Authorization: Bearer $TOKEN"  # obtain TOKEN from ` + s.Options["tokenUrl"]}
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
		return []string{`-H "` + header + `: ` + prefix + `$(` + strings.Join(s.Args, " ") + `)"`}
	}
	return nil
}
