package curlimport

import (
	"strings"
	"testing"
)

func TestParseAndRender(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    string // rendered block, or "" when only warnings matter
		warn    string // a substring one warning must contain
	}{
		{
			name:    "json post with quotes and continuations",
			command: "curl -X POST https://api.example.com/todos \\\n  -H \"Content-Type: application/json\" \\\n  -H 'Authorization: Bearer abc' \\\n  -d '{\"title\": \"it'\\''s\", \"done\": false}'",
			want:    "### POST /todos\n# @name post-todos\n# @assert status == 200\nPOST {{baseUrl}}/todos\nContent-Type: application/json\nAuthorization: Bearer abc\n\n{\"title\": \"it's\", \"done\": false}\n",
		},
		{
			name:    "data implies POST and a form content type; bundled flags; basic auth",
			command: "curl -sSL -u alice:s3cret -d user=alice -d pass=x https://api.example.com/login",
			want:    "### POST /login\n# @name post-login\n# @auth basic alice s3cret\n# @assert status == 200\nPOST {{baseUrl}}/login\nContent-Type: application/x-www-form-urlencoded\n\nuser=alice&pass=x\n",
		},
		{
			name:    "get with data goes to the query; attached short values; --url",
			command: "curl -G --url 'https://api.example.com/search?x=1' --data-urlencode 'q=a b&c' --data-urlencode '=raw' -XGET -A apic-test -e https://ref -b 'sid=1; theme=dark'",
			want:    "### GET /search\n# @name get-search\n# @assert status == 200\nGET {{baseUrl}}/search?x=1&q=a+b%26c&raw\nUser-Agent: apic-test\nReferer: https://ref\nCookie: sid=1; theme=dark\n",
		},
		{
			name:    "multipart form with a file, a type and a literal string",
			command: "curl https://api.example.com/upload -F title=Quarterly -F 'file=@./docs/report.pdf;type=application/pdf' -F 'meta=@x.json;filename=renamed.json' --form-string 'note=@not-a-file'",
			want:    "### POST /upload\n# @name post-upload\n# @assert status == 200\nPOST {{baseUrl}}/upload\nContent-Type: multipart/form-data; boundary=WebAppBoundary\n\n--WebAppBoundary\nContent-Disposition: form-data; name=\"title\"\n\nQuarterly\n--WebAppBoundary\nContent-Disposition: form-data; name=\"file\"; filename=\"report.pdf\"\nContent-Type: application/pdf\n\n< ./docs/report.pdf\n--WebAppBoundary\nContent-Disposition: form-data; name=\"meta\"; filename=\"renamed.json\"\n\n< ./x.json\n--WebAppBoundary\nContent-Disposition: form-data; name=\"note\"\n\n@not-a-file\n--WebAppBoundary--\n",
		},
		{
			name:    "body from a file, insecure, head, unknown flags warn",
			command: "curl -k -I --data-binary @payload.json --bogus --retry 3 -o out.json --cacert ca.pem https://other.example.com/v1/import?dry=1",
			want:    "### HEAD /v1/import\n# @name head-v1-import\n# curl was run with --insecure; run this with `apic --insecure` or set tls.verifyHost in apic.yaml\n# @assert status == 200\nHEAD https://other.example.com/v1/import?dry=1\nContent-Type: application/x-www-form-urlencoded\n\n< ./payload.json\n",
			warn:    "unknown flag --bogus",
		},
		{
			name:    "ansi quoting and a bare host",
			command: "curl example.com/a $'-H' $'X-Note: line1\\nline2' --compressed",
			want:    "### GET /a\n# @name get-a\n# @assert status == 200\nGET http://example.com/a\nX-Note: line1 line2\n",
		},
		{
			name:    "the HTTP version flags go on the request line",
			command: "curl --http2 https://api.example.com/h2",
			want:    "### GET /h2\n# @name get-h2\n# @assert status == 200\nGET {{baseUrl}}/h2 HTTP/2\n",
		},
		{
			name:    "http1.1",
			command: "curl -s --http1.1 https://api.example.com/h1",
			want:    "### GET /h1\n# @name get-h1\n# @assert status == 200\nGET {{baseUrl}}/h1 HTTP/1.1\n",
		},
		{
			name:    "long option with =, empty header and a cookie file",
			command: "curl --request=PUT https://api.example.com/x -H 'X-Empty;' -b cookies.txt --data-raw @literal",
			want:    "### PUT /x\n# @name put-x\n# @assert status == 200\nPUT {{baseUrl}}/x\nX-Empty: \nContent-Type: application/x-www-form-urlencoded\n\n@literal\n",
			warn:    "cookie file",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := Parse(c.command)
			if err != nil {
				t.Fatal(err)
			}
			got := Render(r, Name(r), "https://api.example.com/")
			if c.want != "" && got != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
			if c.warn != "" && !strings.Contains(strings.Join(r.Warnings, "\n"), c.warn) {
				t.Errorf("want a warning containing %q, got %q", c.warn, r.Warnings)
			}
			if c.warn == "" && len(r.Warnings) > 0 && !strings.Contains(c.name, "warn") {
				t.Errorf("unexpected warnings: %q", r.Warnings)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"", "curl -H 'x: y'", "curl 'https://x", "curl \"https://x", "curl -X", "curl https://x \\"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
	if toks, err := tokenize(`a "b c" 'd e' f\ g "h\"i" $'j\tk'`); err != nil || strings.Join(toks, "|") != "a|b c|d e|f g|h\"i|j\tk" {
		t.Errorf("tokens = %q, %v", toks, err)
	}
}

func TestUnknownValueFlagsDoNotEatTheURL(t *testing.T) {
	r, err := Parse("curl --oauth2-bearer eyJ --max-redirs 5 --something odd https://api.example.com/me")
	if err != nil {
		t.Fatal(err)
	}
	if r.URL != "https://api.example.com/me" || len(r.Headers) != 1 || r.Headers[0].Value != "Bearer eyJ" {
		t.Errorf("request = %+v", r)
	}
	if strings.Join(r.Warnings, "\n") != "--max-redirs is about curl's own output or transport and has no place in a request file (ignored)\nunknown flag --something (ignored)\nextra argument \"odd\" (ignored)" {
		t.Errorf("warnings = %q", r.Warnings)
	}
	r, err = Parse(`curl --json '{"a":1}' https://x/j -F 'cv=@résumé.pdf'`)
	if err != nil || r.Method != "POST" || len(r.Parts) != 1 {
		t.Fatalf("--json with a form: %+v %v", r, err)
	}
	if got := Render(r, "j", ""); !strings.Contains(got, `filename="résumé.pdf"`) || strings.Contains(got, `\u`) {
		t.Errorf("non-ASCII filename: %s", got)
	}
	if !SplitsBlock("### heading\ntext") || !SplitsBlock("< 5 items") || SplitsBlock("plain\nbody") {
		t.Error("SplitsBlock")
	}
}
