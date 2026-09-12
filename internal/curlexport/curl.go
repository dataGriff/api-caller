// Package curlexport renders a resolved request as a curl command, so a
// request can still be run where apic is not installed.
package curlexport

import (
	"strings"

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
	parts = append(parts, quote(r.URL))
	return strings.Join(parts, " \\\n  ")
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
