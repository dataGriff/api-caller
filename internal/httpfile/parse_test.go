package httpfile

import (
	"strings"
	"testing"
)

func TestParseSample(t *testing.T) {
	f, diags, err := ParseFile("testdata/sample.http")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Vars) != 2 || f.Vars[0].Name != "baseUrl" || f.Vars[0].Value != "https://api.example.com" {
		t.Fatalf("vars = %+v", f.Vars)
	}
	if len(f.Requests) != 5 {
		t.Fatalf("want 5 requests, got %d", len(f.Requests))
	}

	login := f.Requests[0]
	if login.Name != "login" || login.Method != "POST" || login.URL != "{{baseUrl}}/auth/login" || login.Line != 8 {
		t.Errorf("login = %+v", login)
	}
	if login.Description != "Log in and keep the token" {
		t.Errorf("description fell back wrong: %q", login.Description)
	}
	if len(login.Captures) != 1 || login.Captures[0].Name != "token" || login.Captures[0].Selector != "body.$.access_token" {
		t.Errorf("captures = %+v", login.Captures)
	}
	if login.Body != `{"user": "{{user}}", "password": "{{password}}"}` {
		t.Errorf("body = %q", login.Body)
	}
	if len(login.Headers) != 1 || login.Headers[0].Name != "Content-Type" {
		t.Errorf("headers = %+v", login.Headers)
	}

	get := f.Requests[1]
	if get.URL != "{{baseUrl}}/users/{{userId}}?expand=profile&fields=id,email" {
		t.Errorf("continuation url = %q", get.URL)
	}
	if len(get.Asserts) != 3 || get.Asserts[1].Expr != "body.$.id == {{userId}}" {
		t.Errorf("asserts = %+v", get.Asserts)
	}
	if len(get.Headers) != 2 || get.Headers[1].Name != "Accept" {
		t.Errorf("headers = %+v", get.Headers)
	}
	if get.Description != "Fetch a single user by id" {
		t.Errorf("description = %q", get.Description)
	}
	if get.Body != "" {
		t.Errorf("unexpected body %q", get.Body)
	}

	health := f.Requests[2]
	if health.Method != "GET" || health.URL != "{{baseUrl}}/health" || health.HTTPVersion != "HTTP/1.1" || health.Name != "" {
		t.Errorf("health = %+v", health)
	}
	if health.ID() != "testdata/sample.http#3" {
		t.Errorf("id = %q", health.ID())
	}

	up := f.Requests[3]
	if up.BodyFile != "./payload.json" || up.Body != "" {
		t.Errorf("upload = %+v", up)
	}

	var warnings, errors int
	for _, d := range diags {
		switch d.Severity {
		case "warning":
			warnings++
		case "error":
			errors++
		}
	}
	if warnings != 1 || errors != 1 {
		t.Errorf("diags = %v", diags)
	}
}

func TestParseCRLFAndImplicitGet(t *testing.T) {
	f, _ := Parse("x.http", "GET https://a.b/c\r\nX-A: 1\r\n\r\nhello\r\n\r\n")
	if len(f.Requests) != 1 || f.Requests[0].Body != "hello" || f.Requests[0].Headers[0].Value != "1" {
		t.Fatalf("%+v", f.Requests[0])
	}
}

func TestParseTokenHeaderNames(t *testing.T) {
	f, diags := Parse("t.http", "### a\nGET http://x\nX.Correlation-ID: abc\nX-Api_Key: k\n")
	if len(diags) != 0 || len(f.Requests) != 1 || len(f.Requests[0].Headers) != 2 {
		t.Fatalf("header names may use any RFC 7230 token character: %v %+v", diags, f.Requests)
	}
	if f.Requests[0].Headers[0].Name != "X.Correlation-ID" {
		t.Fatalf("%+v", f.Requests[0].Headers)
	}
	// `#` is a token character too, but a line starting with one is a
	// comment in this dialect; elsewhere in a name it is fine.
	f, diags = Parse("t.http", "### a\nGET http://x\n#X-Trace: v\nX#Y: 1\n")
	if len(diags) != 0 || len(f.Requests) != 1 || len(f.Requests[0].Headers) != 1 || f.Requests[0].Headers[0].Name != "X#Y" {
		t.Fatalf("a leading # is a comment, an inner # is part of the name: %v %+v", diags, f.Requests[0].Headers)
	}
}

// TestEditorHandlerBlocksAreIgnored pins the promise docs/comparison.md makes:
// "apic reads the common subset and ignores what it does not know, so a file
// with editor-only features still parses". Before this, a VS Code REST Client
// or JetBrains response handler was sent as part of the request body — apic
// POSTed the user's script to their own API.
func TestEditorHandlerBlocksAreIgnored(t *testing.T) {
	src := "### Login\n# @name login\nPOST https://api.example.com/login\nContent-Type: application/json\n\n" +
		"{\"user\": \"alice\"}\n\n" +
		"> {%\n    client.global.set(\"token\", response.body.access_token);\n%}\n"
	f, diags := Parse("x.http", src)
	if len(f.Requests) != 1 {
		t.Fatalf("expected one request, got %d", len(f.Requests))
	}
	got := f.Requests[0].Body
	if got != `{"user": "alice"}` {
		t.Fatalf("handler block leaked into the body:\n%q", got)
	}
	for _, bad := range []string{"client.global.set", "%}", "> {%"} {
		if strings.Contains(got, bad) {
			t.Errorf("body should not contain %q:\n%q", bad, got)
		}
	}
	// It is skipped, and said to be skipped — `apic validate` surfaces this.
	if len(diags) != 1 || diags[0].Severity != "warning" {
		t.Fatalf("expected one warning diagnostic, got %+v", diags)
	}
}

// TestPreRequestScriptIsNotABodyFile pins the nastier half: "< {%" used to hit
// the "< ./file" body-file branch and became BodyFile "{%", failing at run
// time with "body file: no such file". A real "< ./body.json" must still work.
func TestPreRequestScriptIsNotABodyFile(t *testing.T) {
	f, _ := Parse("x.http", "# @name a\nPOST https://api.example.com/x\n\n< {%\n  request.variables.set(\"n\", 1);\n%}\n")
	if got := f.Requests[0].BodyFile; got != "" {
		t.Errorf("a pre-request script is not a body file, got BodyFile=%q", got)
	}
	if got := f.Requests[0].Body; got != "" {
		t.Errorf("a pre-request script should not become the body, got %q", got)
	}

	f2, diags := Parse("y.http", "# @name b\nPOST https://api.example.com/x\n\n< ./body.json\n")
	if len(diags) != 0 {
		t.Fatalf("a real body file reference should parse cleanly: %+v", diags)
	}
	if got := f2.Requests[0].BodyFile; got != "./body.json" {
		t.Errorf("BodyFile = %q, want ./body.json", got)
	}
}
