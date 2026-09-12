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
		Headers: []httpfile.Header{{Name: "Authorization", Value: "Bearer it's"}}, Body: `{"a":1}`})
	want := "curl -sS \\\n  -H 'Authorization: Bearer it'\\''s' \\\n  --data-raw '{\"a\":1}' \\\n  'https://a.b/c?x=1'"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if got := Command(&runner.Resolved{Method: "DELETE", URL: "https://a.b"}); got != "curl -sS \\\n  -X DELETE \\\n  'https://a.b'" {
		t.Fatalf("got %q", got)
	}
	if got := Command(&runner.Resolved{Method: "GET", URL: "https://a.b", Body: "x=1"}); got != "curl -sS \\\n  -X GET \\\n  --data-raw 'x=1' \\\n  'https://a.b'" {
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
		"basic u p":                                "--user 'u:p'",
		"bearer t":                                 "-H 'Authorization: Bearer t'",
		"aws region=eu-west-2 service=s3":          "--aws-sigv4 'aws:amz:eu-west-2:s3'",
		"exec gcloud auth print-access-token":      `-H "Authorization: Bearer $(gcloud auth print-access-token)"`,
		"oauth2 tokenUrl=https://idp/t clientId=c": "$TOKEN",
	}
	for spec, want := range cases {
		if got := Command(mk(spec)); !strings.Contains(got, want) {
			t.Errorf("%s:\n%s\nmissing %s", spec, got, want)
		}
	}
}
