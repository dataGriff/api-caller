package curlexport

import (
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/runner"
)

func TestCommand(t *testing.T) {
	got := Command(&runner.Resolved{Method: "POST", URL: "https://a.b/c?x=1",
		Headers: []httpfile.Header{{Name: "Authorization", Value: "******'s"}}, Body: `{"a":1}`}, false)
	want := "curl -sS \\\n  -H 'Authorization: ******'\\''s' \\\n  --data-raw '{\"a\":1}' \\\n  'https://a.b/c?x=1'"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if got := Command(&runner.Resolved{Method: "DELETE", URL: "https://a.b"}, false); got != "curl -sS \\\n  -X DELETE \\\n  'https://a.b'" {
		t.Fatalf("got %q", got)
	}
	if got := Command(&runner.Resolved{Method: "GET", URL: "https://a.b", Body: "x=1"}, false); got != "curl -sS \\\n  -X GET \\\n  --data-raw 'x=1' \\\n  'https://a.b'" {
		t.Fatalf("got %q", got)
	}
}

func TestAuthFlags(t *testing.T) {
	mk := func(spec string) *runner.Resolved {
		s, err := auth.Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		return &runner.Resolved{Method: "GET", URL: "https://a.b", AuthSpec: s}
	}
	cases := map[string]string{
		"basic u p":                           "--user 'u:p'",
		"bearer t":                            "Authorization:",
		"aws region=eu-west-2 service=s3":     "--aws-sigv4 'aws:amz:eu-west-2:s3'",
		"aws":                                 `--aws-sigv4 "aws:amz:$AWS_REGION:execute-api"`,
		"aws service=s3":                      `${AWS_SESSION_TOKEN:+x-amz-security-token:$AWS_SESSION_TOKEN}`,
		"exec gcloud auth print-access-token": "$('gcloud' 'auth' 'print-access-token')",
		"oauth2 tokenUrl=https://idp/t clientId=c": "$TOKEN",
	}
	for spec, want := range cases {
		if got := Command(mk(spec), false); !strings.Contains(got, want) {
			t.Errorf("%s:\n%s\nmissing %s", spec, got, want)
		}
	}
}

func TestOAuth2ExportHasNoInlineComment(t *testing.T) {
	s, err := auth.Parse("oauth2 tokenUrl=https://idp/t clientId=c")
	if err != nil {
		t.Fatal(err)
	}
	got := Command(&runner.Resolved{Method: "GET", URL: "https://a.b", AuthSpec: s}, false)
	if strings.Contains(got, "#") {
		t.Fatalf("unexpected inline comment in curl export:\n%s", got)
	}
}

func TestExecExportQuotesArguments(t *testing.T) {
	s, err := auth.Parse(`exec cmd "arg with space" "quo'te" header=X-Api-Key prefix="pre;fix"`)
	if err != nil {
		t.Fatal(err)
	}
	got := Command(&runner.Resolved{Method: "GET", URL: "https://a.b", AuthSpec: s}, false)
	want := `-H 'X-Api-Key: pre;fix '$('cmd' 'arg with space' 'quo'\''te')`
	if !strings.Contains(got, want) {
		t.Fatalf("got\n%s\nmissing\n%s", got, want)
	}
}

// TestCommandRedacts pins that `apic curl --redact` prints nothing runnable
// against a real API: bearer and basic join the aws/oauth2/exec branches in
// emitting shell placeholders rather than live credentials.
func TestCommandRedacts(t *testing.T) {
	mk := func(spec string) *runner.Resolved {
		s, err := auth.Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		return &runner.Resolved{
			Method: "POST", URL: "https://a.b/c?token=live-token&id=1",
			Headers:       []httpfile.Header{{Name: "X-Api-Key", Value: "live-key"}, {Name: "Accept", Value: "application/json"}},
			SecretHeaders: map[string]bool{"X-Api-Key": true},
			Body:          `{"password":"live-password"}`,
			AuthSpec:      s,
		}
	}
	live := []string{"live-token", "live-key", "live-password", "bearer-secret", "basic-user", "basic-password"}
	for _, spec := range []string{"bearer bearer-secret", "basic basic-user basic-password"} {
		got := Command(mk(spec), true)
		for _, bad := range live {
			if strings.Contains(got, bad) {
				t.Errorf("%s: redacted command leaks %q:\n%s", spec, bad, got)
			}
		}
	}
	if got := Command(mk("bearer bearer-secret"), true); !strings.Contains(got, "$TOKEN") {
		t.Errorf("redacted bearer should use a placeholder:\n%s", got)
	}
	if got := Command(mk("basic basic-user basic-password"), true); !strings.Contains(got, "$APIC_USER:$APIC_PASSWORD") {
		t.Errorf("redacted basic should use placeholders:\n%s", got)
	}
	// Without redact the command stays runnable as printed.
	if got := Command(mk("bearer bearer-secret"), false); !strings.Contains(got, "bearer-secret") {
		t.Errorf("without redact the command should be runnable:\n%s", got)
	}
}

func TestCommandMapsMultipartPartsOntoForm(t *testing.T) {
	r := &runner.Resolved{Method: "POST", URL: "https://a.b/upload",
		Headers: []httpfile.Header{{Name: "Content-Type", Value: "multipart/form-data; boundary=WebAppBoundary"}, {Name: "X-Trace", Value: "1"}},
		Body:    "<multipart: 3 parts, 2 files>",
		Parts: []runner.FormPart{
			{Name: "title", Value: "Quarterly report for alice"},
			{Name: "file", Filename: "report.pdf", ContentType: "application/pdf", File: "files/report.pdf"},
			{Name: "meta", Filename: "renamed.json", File: "files/meta.json"},
		}}
	want := "curl -sS \\\n  -H 'X-Trace: 1' \\\n  --form-string 'title=Quarterly report for alice' \\\n  -F 'file=@files/report.pdf;type=application/pdf' \\\n  -F 'meta=@files/meta.json;filename=renamed.json' \\\n  'https://a.b/upload'"
	if got := Command(r, false); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// Redacted: values masked, paths kept, no body summary leaks in.
	got := Command(r, true)
	if !strings.Contains(got, "--form-string 'title=***'") || !strings.Contains(got, "-F 'file=@files/report.pdf;type=application/pdf'") || strings.Contains(got, "data-raw") {
		t.Errorf("redacted: %s", got)
	}
}

func TestCommandMapsTLSOntoCurlFlags(t *testing.T) {
	r := &runner.Resolved{Method: "GET", URL: "https://api.internal/me", TLS: &runner.TLSInfo{CAFile: "certs/ca.pem", CertFile: "certs/client.pem", KeyFile: "certs/client-key.pem", Insecure: true}}
	want := "curl -sS \\\n  --cacert 'certs/ca.pem' \\\n  --cert 'certs/client.pem' \\\n  --key 'certs/client-key.pem' \\\n  --insecure \\\n  'https://api.internal/me'"
	if got := Command(r, false); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	// One file for both: no --key.
	r.TLS = &runner.TLSInfo{CertFile: "certs/client.pem", KeyFile: "certs/client.pem"}
	if got := Command(r, true); strings.Contains(got, "--key") || !strings.Contains(got, "--cert 'certs/client.pem'") {
		t.Errorf("combined file: %s", got)
	}
}

func TestProxyFlags(t *testing.T) {
	base := &runner.Resolved{Method: "GET", URL: "https://a.b"}
	if got := Command(base, false); strings.Contains(got, "proxy") {
		t.Fatalf("no proxy configured, got %q", got)
	}
	base.Proxy = &runner.ProxyInfo{URL: "http://***@proxy.internal:3128", Source: "apic.yaml"}
	if got := Command(base, true); !strings.Contains(got, "--proxy 'http://***@proxy.internal:3128' \\\n  'https://a.b'") {
		t.Fatalf("redacted proxy: %q", got)
	}
	base.Proxy = &runner.ProxyInfo{Source: "--no-proxy", Off: true}
	if got := Command(base, false); !strings.Contains(got, "--noproxy '*' \\\n  'https://a.b'") {
		t.Fatalf("off: %q", got)
	}
}
