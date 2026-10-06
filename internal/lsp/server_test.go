package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dataGriff/api-caller/internal/history"
)

// client drives a server over in-memory pipes, as an editor would.
type client struct {
	t      *testing.T
	conn   *conn
	nextID int
	done   chan error

	mu      sync.Mutex
	replies map[string]*message
	notes   []*message
	arrived chan struct{}
}

func start(t *testing.T, opts Options) *client {
	t.Helper()
	toServer, fromClient := io.Pipe()
	toClient, fromServer := io.Pipe()
	c := &client{t: t, conn: newConn(toClient, fromClient), done: make(chan error, 1),
		replies: map[string]*message{}, arrived: make(chan struct{}, 1)}
	go func() {
		c.done <- Serve(context.Background(), toServer, fromServer, opts)
		_ = fromServer.Close()
	}()
	go func() {
		for {
			m, err := c.conn.read()
			if err != nil {
				return
			}
			c.mu.Lock()
			if m.Method != "" {
				c.notes = append(c.notes, m)
			} else if m.ID != nil {
				c.replies[string(*m.ID)] = m
			}
			c.mu.Unlock()
			select {
			case c.arrived <- struct{}{}:
			default:
			}
		}
	}()
	t.Cleanup(func() { _ = fromClient.Close() })
	return c
}

// call sends a request and waits for its reply.
func (c *client) call(method string, params any) *message {
	c.t.Helper()
	c.nextID++
	id := json.RawMessage(fmt.Sprint(c.nextID))
	raw, _ := json.Marshal(params)
	if err := c.conn.write(&message{ID: &id, Method: method, Params: raw}); err != nil {
		c.t.Fatal(err)
	}
	var m *message
	c.waitFor(method, func() bool {
		m = c.replies[string(id)]
		return m != nil
	})
	return m
}

func (c *client) notify(method string, params any) {
	c.t.Helper()
	if err := c.conn.notify(method, params); err != nil {
		c.t.Fatal(err)
	}
}

// waitFor polls cond, under the client's lock, until it holds.
func (c *client) waitFor(what string, cond func() bool) {
	c.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		c.mu.Lock()
		ok := cond()
		c.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-c.arrived:
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			c.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// diagnostics waits for the latest diagnostics published for a path
// after the first `after` notifications.
func (c *client) diagnostics(path string, after int) []diagnostic {
	c.t.Helper()
	var out []diagnostic
	uri := pathToURI(path)
	c.waitFor("diagnostics for "+path, func() bool {
		for i := len(c.notes) - 1; i >= after; i-- {
			n := c.notes[i]
			if n.Method != "textDocument/publishDiagnostics" {
				continue
			}
			var p publishDiagnosticsParams
			_ = json.Unmarshal(n.Params, &p)
			if p.URI == uri {
				out = p.Diagnostics
				return true
			}
		}
		return false
	})
	return out
}

func (c *client) noteCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.notes)
}

func result[T any](t *testing.T, m *message) T {
	t.Helper()
	if m.Error != nil {
		t.Fatalf("error reply: %v", m.Error)
	}
	var v T
	raw, _ := json.Marshal(m.Result)
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return v
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const apiHTTP = `### Log in
# @name login
# @assert status == 200
# @capture token = body.$.token
POST {{baseUrl}}/login
Content-Type: application/json

{"user": "{{user}}", "password": "{{password}}"}

### Who am I
# @name whoami
# @ref login
GET {{baseUrl}}/me
Authorization: Bearer {{token}}
`

// fixture is a project with an env file, a secret and two requests, and
// a server initialised on it.
func fixture(t *testing.T, snippets bool) (*client, string, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"t-1","user":{"name":"alice","roles":["admin"]}}`))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "http-client.env.json"), `{"dev": {"baseUrl": "`+srv.URL+`", "user": "alice"}}`)
	writeFile(t, filepath.Join(dir, "http-client.private.env.json"), `{"dev": {"password": "s3cret"}}`)
	writeFile(t, filepath.Join(dir, "apic.yaml"), "env: dev\n")
	file := filepath.Join(dir, "api.http")
	writeFile(t, file, apiHTTP)
	c := start(t, Options{Version: "test"})
	init := c.call("initialize", map[string]any{
		"rootUri":      pathToURI(dir),
		"capabilities": map[string]any{"textDocument": map[string]any{"completion": map[string]any{"completionItem": map[string]any{"snippetSupport": snippets}}}},
	})
	caps := result[map[string]any](t, init)["capabilities"].(map[string]any)
	for _, k := range []string{"completionProvider", "hoverProvider", "codeLensProvider", "documentFormattingProvider", "executeCommandProvider"} {
		if caps[k] == nil {
			t.Errorf("capability %s missing: %v", k, caps)
		}
	}
	c.notify("initialized", map[string]any{})
	return c, dir, file
}

func (c *client) open(path, text string) {
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": pathToURI(path), "languageId": "http", "version": 1, "text": text}})
}

func (c *client) change(path, text string, version int) {
	c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": pathToURI(path), "version": version},
		"contentChanges": []map[string]any{{"text": text}},
	})
}

func TestDiagnosticsFollowTheBuffer(t *testing.T) {
	c, _, file := fixture(t, true)
	c.open(file, apiHTTP)
	if d := c.diagnostics(file, 0); len(d) != 0 {
		t.Fatalf("a clean file has diagnostics: %+v", d)
	}
	// An unsaved edit: a bad assertion on line 3.
	broken := strings.Replace(apiHTTP, "# @assert status == 200", "# @assert", 1)
	n := c.noteCount()
	c.change(file, broken, 2)
	d := c.diagnostics(file, n)
	if len(d) != 1 || d[0].Code != "bad-assert" || d[0].Severity != severityError || d[0].Range.Start.Line != 2 {
		t.Fatalf("diagnostics after the edit = %+v", d)
	}
	if d[0].Range.Start.Character != 2 || d[0].Range.End.Character != 9 {
		t.Errorf("range = %+v, want the @assert key (2..9)", d[0].Range)
	}
	n = c.noteCount()
	c.change(file, apiHTTP, 3)
	if d := c.diagnostics(file, n); len(d) != 0 {
		t.Errorf("fixing it should clear the diagnostics: %+v", d)
	}
}

func TestDiagnosticColumnsAreUTF16(t *testing.T) {
	c, _, file := fixture(t, true)
	// é is two bytes and one UTF-16 unit; 😀 is four bytes and two units.
	text := "### é😀\n# @name café\n# @frobnicate\nGET https://example.com\n"
	c.open(file, text)
	d := c.diagnostics(file, 0)
	if len(d) != 1 || d[0].Code != "unknown-directive" || d[0].Range.Start.Line != 2 {
		t.Fatalf("diagnostics = %+v", d)
	}
	u := units{}
	if got := u.toClient("é😀x", len("é😀")); got != 3 {
		t.Errorf("UTF-16 column of x = %d, want 3", got)
	}
	if got := u.toByte("é😀x", 3); got != len("é😀") {
		t.Errorf("byte offset of column 3 = %d, want %d", got, len("é😀"))
	}
}

func completionLabels(t *testing.T, m *message) (map[string]completionItem, []string) {
	t.Helper()
	list := result[completionList](t, m)
	byLabel := map[string]completionItem{}
	var labels []string
	for _, it := range list.Items {
		byLabel[it.Label] = it
		labels = append(labels, it.Label)
	}
	return byLabel, labels
}

func TestCompletion(t *testing.T) {
	c, _, file := fixture(t, true)
	text := apiHTTP + "\n### New\n# @\nGET {{\n# @assert \n# @auth \n# @ref \n"
	c.open(file, text)
	ls := lines(text)
	at := func(line int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": pathToURI(file)}, "position": map[string]any{"line": line, "character": len(ls[line])}}
	}
	find := func(prefix string) int {
		for i := len(ls) - 1; i >= 0; i-- {
			if ls[i] == prefix {
				return i
			}
		}
		t.Fatalf("no line %q", prefix)
		return 0
	}

	items, labels := completionLabels(t, c.call("textDocument/completion", at(find("# @"))))
	if labels[0] != "@name" || items["@assert"].InsertTextFormat != formatSnippet || items["@retry"].Label == "" {
		t.Errorf("directives = %v", labels)
	}
	if e := items["@name"].TextEdit; e == nil || e.Range.Start.Character != 2 || e.NewText != "@name ${1:request-name}" {
		t.Errorf("@name edit = %+v", e)
	}

	items, _ = completionLabels(t, c.call("textDocument/completion", at(find("GET {{"))))
	if it := items["baseUrl"]; it.Detail == "" || it.TextEdit.NewText != "baseUrl}}" {
		t.Errorf("baseUrl = %+v", it)
	}
	if it := items["password"]; it.Documentation == nil || it.Documentation.Value != "= ***" {
		t.Errorf("a secret's value should be masked: %+v", it)
	}
	if items["$uuid"].Label == "" || items["login.response.body.$"].Label == "" {
		t.Errorf("built-ins and response references missing: %v", items)
	}

	items, _ = completionLabels(t, c.call("textDocument/completion", at(find("# @assert "))))
	if items["status"].Label == "" || items["body.$."].Label == "" {
		t.Errorf("selectors = %v", items)
	}
	items, _ = completionLabels(t, c.call("textDocument/completion", at(find("# @auth "))))
	if items["bearer"].Label == "" || items["aws"].Label == "" {
		t.Errorf("auth types = %v", items)
	}
	items, _ = completionLabels(t, c.call("textDocument/completion", at(find("# @ref "))))
	if items["login"].Label == "" || items["whoami"].Label == "" {
		t.Errorf("ref targets = %v", items)
	}
	none := c.call("textDocument/completion", at(0))
	if none.Error != nil || string(must(json.Marshal(none.Result))) != "null" {
		t.Errorf("a title line offers nothing: %+v", none)
	}
}

func TestCompletionWithoutSnippetSupport(t *testing.T) {
	c, _, file := fixture(t, false)
	text := "# @\n"
	c.open(file, text)
	items, _ := completionLabels(t, c.call("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(file)}, "position": map[string]any{"line": 0, "character": 3}}))
	it := items["@assert"]
	if it.InsertTextFormat != formatPlain || it.TextEdit.NewText != "@assert status == 200" {
		t.Errorf("plain @assert = %+v", it.TextEdit)
	}
}

func TestBodyPathCompletionUsesTheLastRun(t *testing.T) {
	c, _, file := fixture(t, true)
	c.open(file, apiHTTP)
	run := c.call("workspace/executeCommand", map[string]any{"command": CommandRun, "arguments": []any{pathToURI(file), "api.http#login"}})
	if res := result[map[string]any](t, run); res["ok"] != true {
		t.Fatalf("run login = %v", res)
	}
	text := strings.Replace(apiHTTP, "# @capture token = body.$.token", "# @capture token = body.$.user.", 1)
	c.change(file, text, 2)
	line := 3
	items, _ := completionLabels(t, c.call("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(file)}, "position": map[string]any{"line": line, "character": len(lines(text)[line])}}))
	if items["body.$.user.name"].Label == "" || items["body.$.user.roles"].Detail != "array of 1" {
		t.Errorf("body paths after a run = %v", items)
	}
}

func TestHover(t *testing.T) {
	c, _, file := fixture(t, true)
	c.open(file, apiHTTP)
	ls := lines(apiHTTP)
	hoverAt := func(line int, word string) *message {
		return c.call("textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": pathToURI(file)},
			"position":     map[string]any{"line": line, "character": strings.Index(ls[line], "{{"+word) + 2}})
	}
	h := result[hover](t, hoverAt(4, "baseUrl"))
	if !strings.Contains(h.Contents.Value, "**baseUrl** = `http://127.0.0.1") || !strings.Contains(h.Contents.Value, "http-client.env.json") {
		t.Errorf("baseUrl hover = %q", h.Contents.Value)
	}
	if h.Range == nil || h.Range.Start.Character != strings.Index(ls[4], "{{") {
		t.Errorf("hover range = %+v", h.Range)
	}
	h = result[hover](t, hoverAt(7, "password"))
	if !strings.Contains(h.Contents.Value, "`***`") || strings.Contains(h.Contents.Value, "s3cret") || !strings.Contains(h.Contents.Value, "secret, masked") {
		t.Errorf("a secret's hover = %q", h.Contents.Value)
	}
	h = result[hover](t, hoverAt(13, "token"))
	if !strings.Contains(h.Contents.Value, "not set") || !strings.Contains(h.Contents.Value, "which `# @ref` runs first") {
		t.Errorf("a captured variable's hover = %q", h.Contents.Value)
	}
	off := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": pathToURI(file)}, "position": map[string]any{"line": 0, "character": 5}})
	if string(must(json.Marshal(off.Result))) != "null" {
		t.Errorf("hover off a placeholder = %+v", off.Result)
	}
}

func TestCodeLensesAndCommands(t *testing.T) {
	c, _, file := fixture(t, true)
	c.open(file, apiHTTP)
	lenses := result[[]codeLens](t, c.call("textDocument/codeLens", map[string]any{"textDocument": map[string]any{"uri": pathToURI(file)}}))
	if len(lenses) != 6 || lenses[0].Command.Title != "▶ Run login" || lenses[0].Range.Start.Line != 4 || lenses[3].Range.Start.Line != 12 {
		t.Fatalf("lenses = %+v", lenses)
	}
	if args := lenses[4].Command.Arguments; len(args) != 2 || args[1] != "api.http#whoami" || lenses[4].Command.Command != CommandDescribe {
		t.Errorf("describe lens = %+v", lenses[4].Command)
	}

	n := c.noteCount()
	run := c.call("workspace/executeCommand", map[string]any{"command": CommandRun, "arguments": lenses[3].Command.Arguments})
	res := result[map[string]any](t, run)
	if res["ok"] != true || res["ran_first"] == nil {
		t.Errorf("run whoami = %v", res)
	}
	c.waitFor("a summary", func() bool {
		for _, m := range c.notes[n:] {
			if m.Method == "window/showMessage" && strings.Contains(string(m.Params), "✓ whoami · 200 OK") {
				return true
			}
		}
		return false
	})

	d := result[map[string]any](t, c.call("workspace/executeCommand", map[string]any{"command": CommandDescribe, "arguments": lenses[1].Command.Arguments}))
	if d["name"] != "login" || strings.Contains(fmt.Sprint(d), "s3cret") {
		t.Errorf("describe = %v", d)
	}
	curl := result[string](t, c.call("workspace/executeCommand", map[string]any{"command": CommandCurl, "arguments": lenses[2].Command.Arguments}))
	if !strings.HasPrefix(curl, "curl ") || !strings.Contains(curl, "/login") {
		t.Errorf("curl = %q", curl)
	}
	bad := c.call("workspace/executeCommand", map[string]any{"command": "apic.lsp.nope", "arguments": lenses[2].Command.Arguments})
	if bad.Error == nil || bad.Error.Code != codeInvalidParams {
		t.Errorf("an unknown command = %+v", bad.Error)
	}
}

func TestFormatting(t *testing.T) {
	c, _, file := fixture(t, true)
	messy := "###   Log in\n# @name login\npost {{baseUrl}}/login\ncontent-type: application/json\n\n{\"a\":1}\n"
	c.open(file, messy)
	edits := result[[]textEdit](t, c.call("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": pathToURI(file)}, "options": map[string]any{"tabSize": 2, "insertSpaces": true}}))
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "Content-Type: application/json") || edits[0].Range.End.Line != 6 {
		t.Fatalf("edits = %+v", edits)
	}
	c.change(file, edits[0].NewText, 2)
	edits = result[[]textEdit](t, c.call("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": pathToURI(file)}}))
	if len(edits) != 0 {
		t.Errorf("formatting a formatted file = %+v", edits)
	}
}

func TestWatchedFilesRecheckTheProject(t *testing.T) {
	c, dir, file := fixture(t, true)
	c.open(file, apiHTTP)
	c.diagnostics(file, 0)
	// A retry policy that does not parse, written outside the editor.
	cfg := filepath.Join(dir, "apic.yaml")
	writeFile(t, cfg, "env: dev\nretry: lots\n")
	n := c.noteCount()
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]any{{"uri": pathToURI(cfg), "type": 2}}})
	d := c.diagnostics(cfg, n)
	if len(d) != 1 || d[0].Code != "bad-retry" {
		t.Fatalf("apic.yaml diagnostics = %+v", d)
	}
}

func TestLifecycle(t *testing.T) {
	c := start(t, Options{})
	early := c.call("textDocument/hover", map[string]any{})
	if early.Error == nil || early.Error.Code != codeNotInitialized {
		t.Errorf("a request before initialize = %+v", early.Error)
	}
	c.call("initialize", map[string]any{"rootUri": pathToURI(t.TempDir())})
	unknown := c.call("textDocument/references", map[string]any{})
	if unknown.Error == nil || unknown.Error.Code != codeMethodNotFound {
		t.Errorf("an unknown method = %+v", unknown.Error)
	}
	if m := c.call("shutdown", nil); m.Error != nil {
		t.Fatal(m.Error)
	}
	c.notify("exit", nil)
	select {
	case err := <-c.done:
		if err != nil {
			t.Errorf("exit after shutdown = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not exit")
	}

	c = start(t, Options{})
	c.call("initialize", map[string]any{})
	c.notify("exit", nil)
	if err := <-c.done; err == nil {
		t.Error("exit without shutdown should be an error")
	}
}

func TestFramingRejectsABadLength(t *testing.T) {
	c := newConn(strings.NewReader("Content-Length: lots\r\n\r\n{}"), io.Discard)
	if _, err := c.read(); err == nil || !strings.Contains(err.Error(), "Content-Length") {
		t.Errorf("err = %v", err)
	}
	c = newConn(strings.NewReader(fmt.Sprintf("Content-Length: %d\r\n\r\n{}", maxMessage+1)), io.Discard)
	if _, err := c.read(); err == nil {
		t.Error("an oversized message was accepted")
	}
}

func TestPlainSnippet(t *testing.T) {
	for in, want := range map[string]string{
		"assert ${1:status} ${2|==,!=|} ${3:200}": "assert status == 200",
		"$env.${1:NAME}":         "$env.NAME",
		"retry ${1:5} ${2:1s}$0": "retry 5 1s",
	} {
		if got := plainSnippet(in); got != want {
			t.Errorf("plainSnippet(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestURIs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a b", "x.http")
	back, ok := uriToPath(pathToURI(p))
	if !ok || back != p {
		t.Errorf("round trip of %s = %s", p, back)
	}
	if _, ok := uriToPath("untitled:Untitled-1"); ok {
		t.Error("an untitled buffer is not a file")
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestTheWorkspaceIsCheckedWithoutOpeningAFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "apic.yaml"), "env: dev\n")
	warn := filepath.Join(dir, "warn.http")
	writeFile(t, warn, "### w\n# @name w\n# @frobnicate\nGET https://example.com\n")
	c := start(t, Options{})
	c.call("initialize", map[string]any{"rootUri": pathToURI(dir)})
	c.notify("initialized", map[string]any{})
	d := c.diagnostics(warn, 0)
	if len(d) != 1 || d[0].Code != "unknown-directive" {
		t.Fatalf("diagnostics at start = %+v", d)
	}
	// The file is fixed on disk; apic.lsp.validate re-checks and answers
	// once the diagnostics are out.
	writeFile(t, warn, "### w\n# @name w\nGET https://example.com\n")
	n := c.noteCount()
	if m := c.call("workspace/executeCommand", map[string]any{"command": CommandValidate}); m.Error != nil {
		t.Fatal(m.Error)
	}
	if c.noteCount() == n {
		t.Fatal("validate answered before publishing")
	}
	if d := c.diagnostics(warn, n); len(d) != 0 {
		t.Errorf("after the fix = %+v", d)
	}
}

func TestInitializationOptionsTurnFeaturesOff(t *testing.T) {
	c := start(t, Options{})
	init := c.call("initialize", map[string]any{"rootUri": pathToURI(t.TempDir()),
		"initializationOptions": map[string]any{"env": "staging", "codeLens": false, "formatting": false}})
	caps := result[map[string]any](t, init)["capabilities"].(map[string]any)
	if caps["codeLensProvider"] != nil || caps["documentFormattingProvider"] != nil || caps["hoverProvider"] != true {
		t.Errorf("capabilities = %v", caps)
	}
}

func TestBuiltinSnippetsKeepTheirDollar(t *testing.T) {
	for _, snippets := range []bool{true, false} {
		c, _, file := fixture(t, snippets)
		text := apiHTTP + "\n### More\nGET {{\n"
		c.open(file, text)
		last := len(lines(text)) - 2
		items, _ := completionLabels(t, c.call("textDocument/completion", map[string]any{
			"textDocument": map[string]any{"uri": pathToURI(file)}, "position": map[string]any{"line": last, "character": 6}}))
		want := map[bool]map[string]string{
			true:  {"$uuid": `\$uuid}}`, "$env": `\$env.${1:NAME}}}`, "login.response.body.$": `login.response.body.\$.${1:path}}}`},
			false: {"$uuid": "$uuid}}", "$env": "$env.NAME}}", "login.response.body.$": "login.response.body.$.path}}"},
		}[snippets]
		for label, insert := range want {
			it, ok := items[label]
			if !ok {
				t.Errorf("snippets=%v: no %s in %v", snippets, label, items)
				continue
			}
			if got := it.TextEdit.NewText; got != insert {
				t.Errorf("snippets=%v %s inserts %q, want %q", snippets, label, got, insert)
			}
		}
	}
	if got := escapeDollars("assert ${1:status} $0 body.$.x $uuid"); got != `assert ${1:status} $0 body.\$.x \$uuid` {
		t.Errorf("escapeDollars = %q", got)
	}
}

func TestBodyPathCompletionReadsHistoryInTheConfiguredEnvironment(t *testing.T) {
	c, dir, file := fixture(t, true)
	writeFile(t, filepath.Join(dir, "apic.yaml"), "env: dev\nhistory: 3\n")
	// A run from the terminal recorded login under dev, apic.yaml's env;
	// the editor named no environment.
	stored := `{"ok":true,"request":{"name":"login"},"response":{"status":200,"headers":{},"body":{"token":"t","expires":3600},"duration_ms":1,"size":1}}`
	if err := history.New(dir, 3).Record("dev", "login", time.Now(), []byte(stored)); err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(apiHTTP, "# @capture token = body.$.token", "# @capture token = body.$.", 1)
	c.open(file, text)
	items, _ := completionLabels(t, c.call("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(file)}, "position": map[string]any{"line": 3, "character": len(lines(text)[3])}}))
	if items["body.$.expires"].Label == "" || items["body.$.token"].Label == "" {
		t.Errorf("history keys = %v", items)
	}
}

// twoProjects is a workspace folder with no marker of its own holding
// projects a and b, each with a request named login and environments
// dev and staging that point at different hosts.
func twoProjects(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	for _, p := range []string{"a", "b"} {
		writeFile(t, filepath.Join(ws, p, "apic.yaml"), "env: dev\n")
		writeFile(t, filepath.Join(ws, p, "http-client.env.json"), `{"dev": {"host": "`+p+`-dev"}, "staging": {"host": "`+p+`-staging"}}`)
		writeFile(t, filepath.Join(ws, p, "api.http"), "### Log in\n# @name login\nGET https://{{host}}/login\n")
	}
	return ws
}

func hoverValue(t *testing.T, c *client, file string) string {
	t.Helper()
	m := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": pathToURI(file)}, "position": map[string]any{"line": 2, "character": 13}})
	return result[hover](t, m).Contents.Value
}

func TestEnvironmentsArePerProject(t *testing.T) {
	ws := twoProjects(t)
	a, b := filepath.Join(ws, "a", "api.http"), filepath.Join(ws, "b", "api.http")
	c := start(t, Options{})
	c.call("initialize", map[string]any{"rootUri": pathToURI(ws), "initializationOptions": map[string]any{"envs": map[string]string{filepath.Join(ws, "a"): "staging"}}})
	c.notify("initialized", map[string]any{})
	c.open(a, mustRead(t, a))
	c.open(b, mustRead(t, b))
	if v := hoverValue(t, c, a); !strings.Contains(v, "a-staging") {
		t.Errorf("a's host = %q, want staging", v)
	}
	if v := hoverValue(t, c, b); !strings.Contains(v, "b-dev") {
		t.Errorf("b's host = %q, want apic.yaml's dev", v)
	}
	c.notify("workspace/didChangeConfiguration", map[string]any{"settings": map[string]any{"apic": map[string]any{"envs": map[string]string{filepath.Join(ws, "b"): "staging"}}}})
	if v := hoverValue(t, c, b); !strings.Contains(v, "b-staging") {
		t.Errorf("b's host after the change = %q", v)
	}
	if v := hoverValue(t, c, a); !strings.Contains(v, "a-staging") {
		t.Errorf("changing b moved a: %q", v)
	}
}

func TestNestedProjectsKeepTheirOwnDiagnostics(t *testing.T) {
	ws := twoProjects(t)
	a := filepath.Join(ws, "a", "api.http")
	c := start(t, Options{})
	c.call("initialize", map[string]any{"rootUri": pathToURI(ws)})
	c.notify("initialized", map[string]any{})
	// Both projects name a request login; that is no clash, so the
	// workspace check must not publish duplicate-name warnings.
	c.call("workspace/executeCommand", map[string]any{"command": CommandValidate})
	c.mu.Lock()
	for _, n := range c.notes {
		if n.Method == "textDocument/publishDiagnostics" && strings.Contains(string(n.Params), "duplicate-name") {
			t.Errorf("a cross-project duplicate was published: %s", n.Params)
		}
	}
	c.mu.Unlock()
	n := c.noteCount()
	c.open(a, "### Log in\n# @name login\n# @frobnicate\nGET https://{{host}}/login\n")
	if d := c.diagnostics(a, n); len(d) != 1 || d[0].Code != "unknown-directive" {
		t.Errorf("a's own problem = %+v", d)
	}
}

func TestProjectRootsFixTheRoot(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "api", "http-client.env.json"), `{"dev": {"host": "right"}}`)
	writeFile(t, filepath.Join(ws, "api", "requests", "sub", "http-client.env.json"), `{"dev": {"host": "stray"}}`)
	file := filepath.Join(ws, "api", "requests", "sub", "x.http")
	writeFile(t, file, "### X\n# @name x\nGET https://{{host}}/x\n")
	c := start(t, Options{Env: "dev"})
	c.call("initialize", map[string]any{"rootUri": pathToURI(ws), "initializationOptions": map[string]any{"projectRoots": map[string]string{ws: filepath.Join(ws, "api")}}})
	c.open(file, mustRead(t, file))
	if v := hoverValue(t, c, file); !strings.Contains(v, "`right`") {
		t.Errorf("host = %q, want the fixed root's", v)
	}
}

func TestFilesOutsideTheWorkspaceStayInTheirDirectory(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "http-client.env.json"), `{}`)
	s := &server{roots: []string{filepath.Join(home, "work")}, projectDirs: map[string]string{}}
	scratch := filepath.Join(home, "scratch", "t.http")
	if got := s.rootFor(scratch); got != filepath.Dir(scratch) {
		t.Errorf("root of a file outside the workspace = %s, want its directory", got)
	}
	inside := filepath.Join(home, "work", "..build", "x.http")
	if got := s.rootFor(inside); got != filepath.Join(home, "work") {
		t.Errorf("root of %s = %s, want the workspace folder", inside, got)
	}
}

func TestDebouncedEditsAreSettledBeforeARequest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "apic.yaml"), "")
	file := filepath.Join(dir, "api.http")
	writeFile(t, file, "### A\n# @name a\nGET https://example.com\n")
	c := start(t, Options{Debounce: time.Hour})
	c.call("initialize", map[string]any{"rootUri": pathToURI(dir)})
	c.open(file, mustRead(t, file))
	text := "### A\n# @name a\nGET https://example.com\n\n### B\n# @name fresh\nGET https://example.com\n\n### C\n# @ref \nGET https://example.com\n"
	c.change(file, text, 2)
	// The check is an hour away, but the completion settles it first.
	items, _ := completionLabels(t, c.call("textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(file)}, "position": map[string]any{"line": 9, "character": 7}}))
	if items["fresh"].Label == "" {
		t.Errorf("ref targets = %v, want the request just typed", items)
	}
	// Edits pile up behind the debounce; the server still stops at once.
	c.change(file, text+"\n", 3)
	c.change(file, text+"\n\n", 4)
	c.call("shutdown", nil)
	c.notify("exit", nil)
	select {
	case err := <-c.done:
		if err != nil {
			t.Errorf("exit = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not stop with checks pending")
	}
}

func TestCurlLensIsRedacted(t *testing.T) {
	c, _, file := fixture(t, true)
	c.open(file, apiHTTP)
	curl := result[string](t, c.call("workspace/executeCommand", map[string]any{"command": CommandCurl, "arguments": []any{pathToURI(file), "api.http#login"}}))
	if strings.Contains(curl, "s3cret") || !strings.Contains(curl, "/login") {
		t.Errorf("curl = %q", curl)
	}
}

func TestAParseErrorIsAnsweredWithANullID(t *testing.T) {
	var out strings.Builder
	in := "Content-Length: 7\r\n\r\n{nope!}"
	if err := Serve(context.Background(), strings.NewReader(in), &out, Options{}); !errors.Is(err, ErrNoShutdown) {
		t.Errorf("err = %v", err)
	}
	if !strings.Contains(out.String(), `"id":null`) || !strings.Contains(out.String(), `"code":-32700`) {
		t.Errorf("reply = %s", out.String())
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
