package httpfile

import (
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
}
