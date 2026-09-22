package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
)

// A GraphQL request, in either spelling, goes out as a POST with the
// {"query","variables"} envelope, placeholders rendered in both halves
// and the editor's marker header left behind.
func TestGraphQLRequests(t *testing.T) {
	type got struct {
		method, contentType, marker string
		body                        map[string]json.RawMessage
	}
	var seen []got
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g := got{method: r.Method, contentType: r.Header.Get("Content-Type"), marker: r.Header.Get("X-Request-Type")}
		_ = json.NewDecoder(r.Body).Decode(&g.body)
		seen = append(seen, g)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"todos": [{"id": 1}]}}`))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	files := map[string]string{
		"query.graphql": "query { todos { {{field}} } }",
		"api.http": `
@done = true
@field = id

### JetBrains
# @name jet
# @assert body.$.data.todos.# == 1
GRAPHQL ` + srv.URL + `/graphql

query Todos($done: Boolean) {
  todos(done: $done) { {{field}} }
}

{"done": {{done}}}

### REST Client
# @name rest
POST ` + srv.URL + `/graphql
X-REQUEST-TYPE: GraphQL
Content-Type: application/json; charset=utf-8

{ todos { id } }

### From a file
# @name filed
GRAPHQL ` + srv.URL + `/graphql

<@ ./query.graphql

### Lower-case marker
# @name lower
POST ` + srv.URL + `/graphql
x-request-type: graphql

{ todos { id } }

### Broken variables
# @name broken
GRAPHQL ` + srv.URL + `/graphql

{ todos { id } }

{"done": {{done}}, nope}

### Broken without placeholders, so validate can tell
# @name static
GRAPHQL ` + srv.URL + `/graphql

{ todos { id } }

{nope}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(p, Options{NoSession: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"jet", "rest", "filed"} {
		req, err := p.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		res, err := r.Run(context.Background(), req)
		if err != nil || !res.OK {
			t.Fatalf("%s: %v %+v", name, err, res)
		}
		if res.Request.Method != "POST" || !strings.HasPrefix(res.Request.Body, `{"query":`) {
			t.Errorf("%s: resolved as %s %q", name, res.Request.Method, res.Request.Body)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("server saw %d requests", len(seen))
	}
	if seen[0].method != "POST" || seen[0].contentType != "application/json" || seen[0].marker != "" {
		t.Errorf("jet on the wire: %+v", seen[0])
	}
	if string(seen[0].body["query"]) != `"query Todos($done: Boolean) {\n  todos(done: $done) { id }\n}"` || string(seen[0].body["variables"]) != `{"done":true}` {
		t.Errorf("jet body: %s", seen[0].body)
	}
	if seen[1].contentType != "application/json; charset=utf-8" || seen[1].marker != "" || string(seen[1].body["query"]) != `"{ todos { id } }"` {
		t.Errorf("rest on the wire: %+v", seen[1])
	}
	if _, has := seen[1].body["variables"]; has {
		t.Error("no variables block, no variables key")
	}
	if string(seen[2].body["query"]) != `"query { todos { id } }"` {
		t.Errorf("filed body: %s", seen[2].body)
	}
	// The wire form is what describe shows; list keeps GRAPHQL.
	jet, _ := p.Lookup("jet")
	d := r.Describe(jet)
	if d.Method != "POST" || !strings.HasPrefix(d.Body, `{"query": "query Todos`) || !strings.HasSuffix(d.Body, `, "variables": {"done": {{done}}}}`) || d.Headers["Content-Type"] != "application/json" {
		t.Errorf("describe: %s %q %v", d.Method, d.Body, d.Headers)
	}
	if jet.Method != "GRAPHQL" {
		t.Errorf("the source method is %s", jet.Method)
	}
	lower, _ := p.Lookup("lower")
	if d := r.Describe(lower); len(d.Headers) != 1 || d.Headers["Content-Type"] != "application/json" {
		t.Errorf("describe drops the marker whatever its case: %v", d.Headers)
	}
	// Variables that are not JSON once rendered are a usage error before
	// anything is sent.
	broken, _ := p.Lookup("broken")
	if _, err := r.Run(context.Background(), broken); err == nil || !strings.Contains(err.Error(), "variables") || ExitCode(err) != ExitUsage {
		t.Errorf("broken: %v", err)
	}
	if len(seen) != 3 {
		t.Error("the broken request was sent")
	}
	// validate cannot judge variables with placeholders in them, so only
	// the static one is reported, at its body line.
	if diags := p.Validate(); len(diags) != 1 || diags[0].Code != "bad-graphql" || diags[0].Line != 49 {
		t.Errorf("validate: %+v", diags)
	}
}
