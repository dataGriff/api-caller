package runner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
)

// A request asserts its response's shape: types, emptiness, length and a
// JSON Schema read relative to the .http file.
func TestShapeAssertions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 7, "name": "alice", "roles": ["admin"], "meta": {}}`))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	files := map[string]string{
		"schemas/user.json":   `{"type": "object", "required": ["id", "name"], "properties": {"id": {"type": "integer"}, "name": {"type": "string"}}}`,
		"schemas/strict.json": `{"type": "object", "properties": {"id": {"type": "string"}}}`,
		"api/users.http": `
### good
# @name good
# @assert body.$.id isInteger
# @assert body.$.name isString
# @assert body.$.roles length == 1
# @assert body.$.meta isEmpty
# @assert body.$.roles not isEmpty
# @assert body.$ matchesSchema ../schemas/user.json
GET ` + srv.URL + `/u

### bad
# @name bad
# @assert body.$.id isString
# @assert body.$ matchesSchema ../schemas/strict.json
GET ` + srv.URL + `/u

### outside
# @name outside
# @assert body.$ matchesSchema ../../elsewhere.json
GET ` + srv.URL + `/u
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
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
	run := func(name string) *Result {
		req, err := p.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		res, err := r.Run(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := run("good"); !res.OK {
		t.Fatalf("good: %+v", res.Asserts)
	}
	res := run("bad")
	if res.OK || len(res.Asserts) != 2 || res.Asserts[0].Actual != "number" || res.Asserts[1].Pass || !strings.Contains(res.Asserts[1].Actual, "id") {
		t.Fatalf("bad: %+v", res.Asserts)
	}
	res = run("outside")
	if res.OK || !strings.Contains(res.Asserts[0].Error, "outside project root") {
		t.Fatalf("outside: %+v", res.Asserts)
	}
	// validate: the missing and the escaping schema, at the path's span.
	var codes []string
	for _, d := range p.Validate() {
		codes = append(codes, d.Code)
		if d.Code == "missing-schema-file" && (d.Line != 20 || d.Column != 32 || d.EndColumn != 52) {
			t.Errorf("span: %+v", d)
		}
	}
	if strings.Join(codes, ",") != "missing-schema-file" {
		t.Errorf("validate: %v", codes)
	}
}
