package httpfile

import (
	"strings"
	"testing"
	"time"
)

func TestParseSample(t *testing.T) {
	f, diags, err := ParseFile("testdata/sample.http")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Vars) != 2 || f.Vars[0].Name != "baseUrl" || f.Vars[0].Value != "https://api.example.com" {
		t.Fatalf("vars = %+v", f.Vars)
	}
	if len(f.Requests) != 9 {
		t.Fatalf("want 9 requests, got %d", len(f.Requests))
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
	if refs := get.Refs(); len(refs) != 1 || refs[0] != (Ref{ID: "login", Line: 16, Column: 8}) {
		t.Errorf("refs = %+v", refs)
	}
	if refs := login.Refs(); refs != nil {
		t.Errorf("login should have no refs, got %+v", refs)
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
	if v, ok := up.Directive("retry"); !ok || v != "3 500ms" {
		t.Errorf("retry directive = %q, %v", v, ok)
	}

	if up.Disabled() {
		t.Error("upload is not disabled")
	}
	if d, err := up.Sleep(); d != 0 || err != nil {
		t.Errorf("upload sleep = %v, %v", d, err)
	}
	slow := f.Requests[8]
	if d, err := slow.Sleep(); !slow.Disabled() || d != 1500*time.Millisecond || err != nil {
		t.Errorf("slow-report: disabled=%v sleep=%v err=%v", slow.Disabled(), d, err)
	}
	for _, bad := range []string{"soon", "-1s", ""} {
		if _, err := ParseSleep(bad); err == nil {
			t.Errorf("ParseSleep(%q) should fail", bad)
		}
	}

	gql := f.Requests[6]
	if gql.Method != "GRAPHQL" || !gql.IsGraphQL() || gql.Name != "todos-by-state" {
		t.Errorf("graphql = %+v", gql)
	}
	if q, v, err := gql.GraphQL(); err != nil || q != "query Todos($done: Boolean) {\n  todos(done: $done) { id title }\n}" || v != `{"done": {{done}}}` {
		t.Errorf("graphql split = %q %q %v", q, v, err)
	}
	rc := f.Requests[7]
	if rc.Method != "POST" || !rc.IsGraphQL() {
		t.Errorf("rest client graphql = %+v", rc)
	}
	if q, v, err := rc.GraphQL(); err != nil || q != "{ todos { id } }" || v != "" {
		t.Errorf("rest client split = %q %q %v", q, v, err)
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
	// Diagnostics carry a code and the span of the offending text, so an
	// editor can underline `@frobnicate` and `nope` rather than whole lines.
	want := []Diagnostic{
		{Path: "testdata/sample.http", Line: 20, Column: 3, EndLine: 20, EndColumn: 14, Severity: "warning", Code: "unknown-directive", Message: "unknown directive @frobnicate (ignored)"},
		{Path: "testdata/sample.http", Line: 41, Column: 12, EndLine: 41, EndColumn: 16, Severity: "error", Code: "bad-capture", Message: "@capture must look like `name = selector`, got \"nope\""},
	}
	for i, w := range want {
		if i >= len(diags) || diags[i] != w {
			t.Errorf("diag %d = %+v, want %+v", i, diags[i], w)
		}
	}
	if d := want[1]; d.String() != "testdata/sample.http:41:12: error: @capture must look like `name = selector`, got \"nope\"" {
		t.Errorf("String() = %q", d.String())
	}
	// Columns on the AST point at the parts later checks report on.
	if c := login.Captures[0]; c.Column != 20 {
		t.Errorf("capture selector column = %d, want 20 (%q)", c.Column, "# @capture token = body.$.access_token")
	}
	if a := get.Asserts[0]; a.Column != 11 {
		t.Errorf("assert expr column = %d, want 11", a.Column)
	}
	if up.BodyFileLine != 37 || up.BodyFileColumn != 3 || up.BodyLine != 37 {
		t.Errorf("body file position = %d:%d (body line %d), want 37:3", up.BodyFileLine, up.BodyFileColumn, up.BodyLine)
	}
	if login.BodyLine != 11 || health.BodyLine != 0 {
		t.Errorf("body lines = %d, %d, want 11, 0", login.BodyLine, health.BodyLine)
	}

	// A multipart body is kept verbatim and read into parts on demand: a
	// text part is a template, a `< file` part reads the file.
	mp := f.Requests[5]
	if !mp.IsMultipart() || mp.BodyFile != "" || !strings.HasPrefix(mp.Body, "--WebAppBoundary\n") {
		t.Fatalf("multipart request = %+v", mp)
	}
	m, err := mp.Multipart()
	if err != nil {
		t.Fatal(err)
	}
	if m.Boundary != "WebAppBoundary" || len(m.Parts) != 2 || m.Files() != 1 {
		t.Fatalf("multipart = %+v", m)
	}
	title, file := m.Parts[0], m.Parts[1]
	if title.Name != "title" || title.Body != "Quarterly report for {{user}}" || title.File != "" || title.Line != 49 {
		t.Errorf("title part = %+v", title)
	}
	if file.Name != "file" || file.Filename != "report.pdf" || file.ContentType != "application/pdf" || file.File != "./report.pdf" || file.Body != "" {
		t.Errorf("file part = %+v", file)
	}
	if file.FileLine != 57 || file.FileColumn != 3 || file.Line != 53 {
		t.Errorf("file part position = %d:%d (delimiter %d), want 57:3 (53)", file.FileLine, file.FileColumn, file.Line)
	}
	if refs := mp.BodyFiles(); len(refs) != 1 || refs[0] != (FileRef{Path: "./report.pdf", Line: 57, Column: 3}) {
		t.Errorf("body files = %+v", refs)
	}
	if refs := up.BodyFiles(); len(refs) != 1 || refs[0] != (FileRef{Path: "./payload.json", Line: 37, Column: 3}) {
		t.Errorf("whole-body file = %+v", refs)
	}
	if login.IsMultipart() {
		t.Error("a JSON request is not multipart")
	}
	if m, err := login.Multipart(); m != nil || err != nil {
		t.Errorf("Multipart() on a JSON request = %v, %v", m, err)
	}
}

func TestMultipartErrors(t *testing.T) {
	cases := map[string]string{
		"no boundary":   "POST http://x\nContent-Type: multipart/form-data\n\n--a\nContent-Disposition: form-data; name=\"a\"\n\n1\n--a--\n",
		"empty":         "POST http://x\nContent-Type: multipart/form-data; boundary=a\n\n",
		"no delimiter":  "POST http://x\nContent-Type: multipart/form-data; boundary=a\n\nplain text\n",
		"not closed":    "POST http://x\nContent-Type: multipart/form-data; boundary=a\n\n--a\nContent-Disposition: form-data; name=\"a\"\n\n1\n",
		"bad header":    "POST http://x\nContent-Type: multipart/form-data; boundary=a\n\n--a\nnot a header\n\n1\n--a--\n",
		"no parts":      "POST http://x\nContent-Type: multipart/form-data; boundary=a\n\n--a--\n",
		"stray content": "POST http://x\nContent-Type: multipart/form-data; boundary=a\n\n--a\nContent-Disposition: form-data; name=\"a\"\n\n1\n--a\n--b\n--a--\n",
	}
	for name, src := range cases {
		f, _ := Parse("x.http", src)
		if len(f.Requests) != 1 {
			t.Fatalf("%s: %d requests", name, len(f.Requests))
		}
		if _, err := f.Requests[0].Multipart(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	// A whole-body file under a multipart content type is the editors'
	// other spelling: the file holds the encoded body, so there are no parts.
	f, _ := Parse("x.http", "POST http://x\nContent-Type: multipart/form-data; boundary=a\n\n< ./body.bin\n")
	if m, err := f.Requests[0].Multipart(); m != nil || err != nil {
		t.Errorf("whole-body file = %v, %v", m, err)
	}
	// Multi-line text content keeps its inner line breaks; the newline
	// before a delimiter belongs to the delimiter.
	f, _ = Parse("x.http", "POST http://x\nContent-Type: multipart/form-data; boundary=a\n\n--a\nContent-Disposition: form-data; name=\"a\"\n\nline 1\n\nline 3\n--a--\n")
	m, err := f.Requests[0].Multipart()
	if err != nil || len(m.Parts) != 1 || m.Parts[0].Body != "line 1\n\nline 3" {
		t.Errorf("multi-line part = %+v, %v", m, err)
	}
}

func TestSpanFindsValueAfterKey(t *testing.T) {
	// `# @name name`: the value repeats the key, and must be found after it.
	f, _ := Parse("x.http", "# @name name\nGET http://x\n")
	if d := f.Requests[0].Directives[0]; d.Column != 9 {
		t.Fatalf("column = %d, want 9", d.Column)
	}
	if col, end := Span("  X: 1", "X: 1", 0); col != 3 || end != 7 {
		t.Fatalf("span = %d,%d", col, end)
	}
	if col, end := Span("abc", "", 0); col != 0 || end != 0 {
		t.Fatalf("empty span = %d,%d", col, end)
	}
	if col, end := Span("abc", "zzz", 0); col != 0 || end != 0 {
		t.Fatalf("missing span = %d,%d", col, end)
	}
}

func TestDiagnosticSpans(t *testing.T) {
	src := "### orphan\n# @name a\n\n### b\n# @name\nGET http://x\n  Bogus header line\n\n> {% client.log(1) %}\n"
	_, diags := Parse("x.http", src)
	got := map[string]Diagnostic{}
	for _, d := range diags {
		got[d.Code] = d
	}
	cases := map[string]Diagnostic{
		"orphan-directives": {Line: 2, Column: 3, EndColumn: 8},
		"bad-name":          {Line: 5, Column: 3, EndColumn: 8},
		"bad-header":        {Line: 7, Column: 3, EndColumn: 20},
		"editor-script":     {Line: 9, Column: 1, EndColumn: 22},
	}
	for code, want := range cases {
		d, ok := got[code]
		if !ok {
			t.Errorf("no %s diagnostic in %+v", code, diags)
			continue
		}
		if d.Line != want.Line || d.Column != want.Column || d.EndLine != want.Line || d.EndColumn != want.EndColumn {
			t.Errorf("%s at %d:%d-%d:%d, want %d:%d-%d", code, d.Line, d.Column, d.EndLine, d.EndColumn, want.Line, want.Column, want.EndColumn)
		}
	}
	for code := range got {
		if _, ok := Codes[code]; !ok {
			t.Errorf("code %q is not in Codes", code)
		}
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

func TestGraphQL(t *testing.T) {
	// The variables are the object after the last blank line; a blank line
	// inside the query is not a split point unless an object follows it.
	q, v := SplitGraphQL("query {\n  a\n}\n\n{\"x\": 1}")
	if q != "query {\n  a\n}" || v != `{"x": 1}` {
		t.Fatalf("%q %q", q, v)
	}
	q, v = SplitGraphQL("query {\n  a\n}\n\nfragment F on T { b }")
	if q != "query {\n  a\n}\n\nfragment F on T { b }" || v != "" {
		t.Fatalf("%q %q", q, v)
	}
	env, err := GraphQLEnvelope("{ a }", `{"x": 1}`)
	if err != nil || env != `{"query":"{ a }","variables":{"x":1}}` {
		t.Fatalf("%q %v", env, err)
	}
	if env, err := GraphQLEnvelope("{ a }", ""); err != nil || env != `{"query":"{ a }"}` {
		t.Fatalf("%q %v", env, err)
	}
	if _, err := GraphQLEnvelope("{ a }", `[1]`); err == nil {
		t.Fatal("an array is not a variables object")
	}
	for _, bad := range []string{"GRAPHQL http://x\n", "GRAPHQL http://x\n\n{ a }\n\n{nope}\n"} {
		f, _ := Parse("g.http", bad)
		if _, _, err := f.Requests[0].GraphQL(); err == nil {
			t.Errorf("%q should not be a valid GraphQL request", bad)
		}
	}
	f, _ := Parse("g.http", "POST http://x\nx-request-type: graphql\n\n{ a }\n")
	if !f.Requests[0].IsGraphQL() {
		t.Error("the header is case-insensitive")
	}
	f, _ = Parse("g.http", "POST http://x\n\n{ a }\n")
	if f.Requests[0].IsGraphQL() {
		t.Error("a plain POST is not GraphQL")
	}
}

func TestSaveTo(t *testing.T) {
	src := "### a\n# @name a\nGET http://x\n\n>> ./out/a.json\n\n### b\n# @name b\nPOST http://x\n\n{\"k\": 1}\n\n>>! ../b.bin\n\n### c\nGET http://x\n\n>>\n\n### d\nGET http://x\n\n>> one.txt\n>>! two.txt\n"
	f, diags := Parse("s.http", src)
	if len(f.Requests) != 4 {
		t.Fatalf("%d requests", len(f.Requests))
	}
	a, b, c, d := f.Requests[0], f.Requests[1], f.Requests[2], f.Requests[3]
	if a.SaveTo == nil || *a.SaveTo != (SaveTo{Path: "./out/a.json", Line: 5, Column: 4}) || a.Body != "" {
		t.Errorf("a = %+v body %q", a.SaveTo, a.Body)
	}
	if b.SaveTo == nil || *b.SaveTo != (SaveTo{Path: "../b.bin", Overwrite: true, Line: 13, Column: 5}) || b.Body != `{"k": 1}` {
		t.Errorf("b = %+v body %q", b.SaveTo, b.Body)
	}
	if c.SaveTo != nil || d.SaveTo == nil || d.SaveTo.Path != "one.txt" {
		t.Errorf("c = %+v d = %+v", c.SaveTo, d.SaveTo)
	}
	var codes []string
	for _, dg := range diags {
		codes = append(codes, dg.Code)
		if dg.Code == "bad-save-path" && dg.Line == 23 && (dg.Column != 5 || dg.EndColumn != 12) {
			t.Errorf("second >> span: %+v", dg)
		}
	}
	if strings.Join(codes, ",") != "bad-save-path,bad-save-path" {
		t.Errorf("codes = %v (%+v)", codes, diags)
	}
	// Not an editor script any more: no warning for a redirect line.
	for _, dg := range diags {
		if dg.Code == "editor-script" {
			t.Errorf("redirect reported as a script: %+v", dg)
		}
	}
}
