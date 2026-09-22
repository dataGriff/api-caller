package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// list keeps the GRAPHQL method the file wrote; curl and describe show
// the POST with the JSON envelope that is sent.
func TestGraphQLCommands(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), `### Todos
# @name todos
GRAPHQL {{baseUrl}}/graphql

query Todos($done: Boolean) {
  todos(done: $done) { id }
}

{"done": true}
`)
	mustWrite(t, filepath.Join(dir, "http-client.env.json"), `{"dev":{"baseUrl":"http://api.test"}}`)
	code, out, errb := execute(t, "list", "-C", dir, "--env", "dev", "--no-color")
	if code != 0 || !strings.Contains(out, "GRAPHQL") {
		t.Fatalf("list: code=%d out=%s err=%s", code, out, errb)
	}
	code, out, errb = execute(t, "curl", "todos", "-C", dir, "--env", "dev")
	if code != 0 {
		t.Fatalf("curl: code=%d out=%s err=%s", code, out, errb)
	}
	// curl infers POST from the body, so no -X is needed.
	for _, want := range []string{"http://api.test/graphql", "Content-Type: application/json", `--data-raw '{"query":"query Todos($done: Boolean) {\n  todos(done: $done) { id }\n}","variables":{"done":true}}'`} {
		if !strings.Contains(out, want) {
			t.Errorf("curl lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "X-REQUEST-TYPE") || strings.Contains(out, "GRAPHQL") {
		t.Errorf("curl carries the editor's marker:\n%s", out)
	}
	code, out, errb = execute(t, "describe", "todos", "-C", dir, "--env", "dev", "--json")
	if code != 0 || !strings.Contains(out, `"method": "POST"`) || !strings.Contains(out, `"body": "{\"query\": \"query Todos`) {
		t.Fatalf("describe: code=%d out=%s err=%s", code, out, errb)
	}
}
