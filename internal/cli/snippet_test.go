package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// apic snippet renders each language; curl stays the alias with its own
// JSON shape; a missing variable or an unknown language is refused.
func TestSnippet(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "### get\n# @name get-user\n# @auth bearer {{token}}\nGET {{baseUrl}}/users/1\n\n### gap\n# @name gap\nGET {{baseUrl}}/{{nope}}\n")
	mustWrite(t, filepath.Join(dir, "http-client.env.json"), `{"dev": {"baseUrl": "https://api.example.com", "token": "t-1"}}`)
	args := []string{"-C", dir, "--env", "dev", "--no-session"}
	code, out, errb := execute(t, append([]string{"snippet", "get-user", "--lang", "python"}, args...)...)
	if code != 0 || !strings.Contains(out, `"Authorization": "Bearer t-1"`) || !strings.Contains(out, "requests.request(") {
		t.Fatalf("python: code=%d out=%s err=%s", code, out, errb)
	}
	code, out, _ = execute(t, append([]string{"snippet", "get-user", "-l", "js", "--redact", "--json"}, args...)...)
	if code != 0 || !strings.Contains(out, `"lang": "js"`) || !strings.Contains(out, "process.env.TOKEN") || strings.Contains(out, "t-1") {
		t.Fatalf("js --json --redact: code=%d out=%s", code, out)
	}
	code, curlOut, _ := execute(t, append([]string{"curl", "get-user"}, args...)...)
	_, snipOut, _ := execute(t, append([]string{"snippet", "get-user"}, args...)...)
	if code != 0 || curlOut != snipOut || !strings.HasPrefix(curlOut, "curl -sS") {
		t.Fatalf("curl alias: %q vs %q", curlOut, snipOut)
	}
	if code, out, _ := execute(t, append([]string{"curl", "get-user", "--json"}, args...)...); code != 0 || !strings.Contains(out, `"command": "curl -sS`) {
		t.Fatalf("curl --json keeps its shape: %s", out)
	}
	if code, _, errb := execute(t, append([]string{"snippet", "get-user", "--lang", "cobol"}, args...)...); code != 2 || !strings.Contains(errb, "curl, httpie, powershell, python, js, go") {
		t.Fatalf("unknown language: code=%d err=%s", code, errb)
	}
	if code, _, errb := execute(t, append([]string{"snippet", "gap", "--lang", "go"}, args...)...); code != 2 || !strings.Contains(errb, "nope") {
		t.Fatalf("missing variable: code=%d err=%s", code, errb)
	}
}
