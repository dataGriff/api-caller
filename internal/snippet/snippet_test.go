package snippet

import (
	"flag"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/runner"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func spec(t *testing.T, raw string) *auth.Spec {
	t.Helper()
	s, err := auth.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// fixtures cover a JSON body with a bearer token, a multipart upload with
// basic auth, an API key in the query with TLS, a proxy and HTTP/1.1, and
// the credentials a language cannot express itself.
func fixtures(t *testing.T) map[string]*runner.Resolved {
	return map[string]*runner.Resolved{
		"json": {Method: "POST", URL: "https://api.example.com/todos?list=home&q=it's",
			Headers:  []httpfile.Header{{Name: "Content-Type", Value: "application/json"}, {Name: "Accept", Value: "application/json"}, {Name: "X-Note", Value: `say "hi" $HOME`}},
			Body:     "{\"title\": \"café \\\"x\\\"\",\n \"done\": false}",
			AuthSpec: spec(t, "bearer s3cret-token")},
		"multipart": {Method: "POST", URL: "http://localhost:8080/upload",
			Headers:  []httpfile.Header{{Name: "Content-Type", Value: "multipart/form-data; boundary=X"}},
			Parts:    []runner.FormPart{{Name: "title", Value: "Q3 report"}, {Name: "file", File: "docs/report.pdf", ContentType: "application/pdf", Filename: "q3.pdf"}},
			AuthSpec: &auth.Spec{Type: "basic", Args: []string{"alice", "pa'ss"}}},
		"apikey": {Method: "GET", URL: "https://internal.example.com/v1/items", HTTPVersion: "HTTP/1.1",
			AuthSpec: spec(t, "apikey k-123 query=api_key"),
			TLS:      &runner.TLSInfo{Insecure: true},
			Proxy:    &runner.ProxyInfo{URL: "http://proxy.internal:3128", Source: "--proxy"}},
		"notes": {Method: "PURGE", URL: "https://cdn.example.com/x", HTTPVersion: "HTTP/2",
			AuthSpec: spec(t, "oauth2 tokenUrl=https://idp/token clientId=c"),
			TLS:      &runner.TLSInfo{CAFile: "certs/ca.pem", CertFile: "certs/client.pem"}},
		"digest": {Method: "DELETE", URL: "https://api.example.com/items/7", AuthSpec: spec(t, "digest bob hunter2")},
		"aws":    {Method: "GET", URL: "https://abc.execute-api.eu-west-2.amazonaws.com/prod/orders", AuthSpec: spec(t, "aws region=eu-west-2")},
		"exec":   {Method: "GET", URL: "https://api.example.com/me", AuthSpec: spec(t, "exec gcloud auth print-access-token header=X-Token prefix=")},
	}
}

// Each language's snippet for each fixture, with and without --redact,
// against a golden file; `go test ./internal/snippet -update` rewrites them.
func TestGolden(t *testing.T) {
	for name, r := range fixtures(t) {
		for _, redact := range []bool{false, true} {
			for _, lang := range Languages {
				got, err := Render(lang, r, redact)
				if err != nil {
					t.Fatalf("%s %s: %v", name, lang, err)
				}
				file := name
				if redact {
					file += ".redacted"
				}
				path := filepath.Join("testdata", file+"."+lang)
				if *update {
					if err := os.MkdirAll("testdata", 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("%v (run with -update)", err)
				}
				if got+"\n" != strings.ReplaceAll(string(want), "\r\n", "\n") {
					t.Errorf("%s differs:\n%s", path, got)
				}
				if redact && (strings.Contains(got, "s3cret") || strings.Contains(got, "hunter2") || strings.Contains(got, "k-123") || strings.Contains(got, "pa'ss")) {
					t.Errorf("%s leaks a credential:\n%s", path, got)
				}
			}
		}
	}
}

// The snippets parse in their own languages, where the interpreter is
// installed: Python's ast, node --check on an ES module, bash -n; the Go
// one is exactly what gofmt would write.
func TestSnippetsParse(t *testing.T) {
	for name, r := range fixtures(t) {
		for _, redact := range []bool{false, true} {
			src, err := Render("go", r, redact)
			if err != nil {
				t.Fatal(err)
			}
			formatted, err := format.Source([]byte(src))
			if err != nil || string(formatted) != src+"\n" {
				t.Errorf("go %s (redact %v) is not gofmt'd: %v\n%s", name, redact, err, src)
			}
		}
	}
	checks := map[string]func(string, string) *exec.Cmd{
		"python": func(dir, src string) *exec.Cmd {
			return exec.Command("python3", "-c", "import ast,sys; ast.parse(sys.stdin.read())")
		},
		"js": func(dir, src string) *exec.Cmd {
			f := filepath.Join(dir, "snippet.mjs")
			if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			return exec.Command("node", "--check", f)
		},
		"curl":   func(string, string) *exec.Cmd { return exec.Command("bash", "-n") },
		"httpie": func(string, string) *exec.Cmd { return exec.Command("bash", "-n") },
	}
	bins := map[string]string{"python": "python3", "js": "node", "curl": "bash", "httpie": "bash"}
	for lang, check := range checks {
		if _, err := exec.LookPath(bins[lang]); err != nil {
			t.Logf("%s: %s not installed, not checked", lang, bins[lang])
			continue
		}
		for name, r := range fixtures(t) {
			for _, redact := range []bool{false, true} {
				src, err := Render(lang, r, redact)
				if err != nil {
					t.Fatal(err)
				}
				cmd := check(t.TempDir(), src)
				cmd.Stdin = strings.NewReader(src)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Errorf("%s %s (redact %v) does not parse: %v\n%s\n%s", lang, name, redact, err, out, src)
				}
			}
		}
	}
	if _, err := Render("cobol", fixtures(t)["json"], false); err == nil || !strings.Contains(err.Error(), "curl, httpie") {
		t.Fatalf("unknown language: %v", err)
	}
}
