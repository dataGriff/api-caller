package curlexport

import (
	"testing"

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
}
