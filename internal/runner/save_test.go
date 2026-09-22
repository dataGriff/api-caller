package runner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
)

var pngish = append([]byte{0x89, 'P', 'N', 'G', 0, 0xff, 0xfe}, bytes.Repeat([]byte{0x00, 0x80, 0xff}, 40)...)

// A `>> file` line saves the response bytes as they came, confined to
// the project, created 0600 when a secret went into the request; `>>`
// refuses an existing file and `>>!` overwrites it.
func TestSaveResponseToFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngish)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok": true}`))
		}
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	files := map[string]string{
		"http-client.private.env.json": `{"dev": {"apiKey": "s3cret"}}`,
		"sub/api.http": `
### Public image
# @name image
GET ` + srv.URL + `/image

>> ../out/logo.png

### Image again, refusing to overwrite
# @name again
GET ` + srv.URL + `/image

>> ../out/logo.png

### Overwrite with a secret in play
# @name secret
GET ` + srv.URL + `/data
X-Api-Key: {{apiKey}}

>>! ../out/data.json

### Escapes the project
# @name escape
GET ` + srv.URL + `/data

>> ../../../escape.json
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
	r, err := New(p, Options{Env: "dev", NoSession: true})
	if err != nil {
		t.Fatal(err)
	}
	run := func(name string) (*Result, error) {
		req, err := p.Lookup(name)
		if err != nil {
			t.Fatal(err)
		}
		return r.Run(context.Background(), req)
	}
	res, err := run("image")
	if err != nil || !res.OK || res.SavedTo != "out/logo.png" {
		t.Fatalf("image: %v %+v", err, res)
	}
	got, err := os.ReadFile(filepath.Join(dir, "out", "logo.png"))
	if err != nil || !bytes.Equal(got, pngish) {
		t.Fatalf("saved bytes differ: %v %d vs %d", err, len(got), len(pngish))
	}
	// The JSON view carries the bytes as base64 and says so; the text
	// output summarises instead of printing them.
	out, _ := json.Marshal(res)
	var obj struct {
		Response struct {
			Body         string `json:"body"`
			BodyEncoding string `json:"body_encoding"`
		} `json:"response"`
		SavedTo string `json:"saved_to"`
	}
	if err := json.Unmarshal(out, &obj); err != nil || obj.Response.BodyEncoding != "base64" || obj.Response.Body != base64.StdEncoding.EncodeToString(pngish) || obj.SavedTo != "out/logo.png" {
		t.Fatalf("json: %v %s", err, out)
	}
	if !IsBinary(res.DisplayRawBody()) {
		t.Fatal("a PNG is binary")
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(filepath.Join(dir, "out", "logo.png")); st.Mode().Perm() != 0o644 {
			t.Errorf("no secret went in: mode %o", st.Mode().Perm())
		}
	}
	// The same file again without `!` is refused, and the request still
	// counts as failed so CI notices.
	res, err = run("again")
	if err != nil || res.OK || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "use >>! to overwrite") || res.SavedTo != "" {
		t.Fatalf("again: %v %+v", err, res)
	}
	// Overwrite, with a secret header: 0600.
	if err := os.WriteFile(filepath.Join(dir, "out", "data.json"), []byte("old"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	res, err = run("secret")
	if err != nil || !res.OK || res.SavedTo != "out/data.json" {
		t.Fatalf("secret: %v %+v", err, res)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "out", "data.json")); string(got) != `{"ok": true}` {
		t.Fatalf("not overwritten: %q", got)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(filepath.Join(dir, "out", "data.json")); st.Mode().Perm() != 0o600 {
			t.Errorf("a secret went in: mode %o", st.Mode().Perm())
		}
	}
	// A path outside the project is refused before anything is written.
	res, err = run("escape")
	if err != nil || res.OK || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "outside project root") {
		t.Fatalf("escape: %v %+v", err, res)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escape.json")); err == nil {
		t.Fatal("escaped the project")
	}
	if diags := p.Validate(); len(diags) != 1 || diags[0].Code != "bad-save-path" || diags[0].Line != 25 || diags[0].Column != 4 {
		t.Fatalf("validate: %+v", diags)
	}
	d := r.Describe(p.Requests()[2])
	if d.SaveTo != "../out/data.json (overwrite)" {
		t.Fatalf("describe: %q", d.SaveTo)
	}

	// --output: a path from the working directory, overwriting.
	target := filepath.Join(t.TempDir(), "deep", "logo.png")
	r.Opts.Output = target
	res, err = run("secret")
	if err != nil || !res.OK || res.SavedTo != target {
		t.Fatalf("--output: %v %+v", err, res)
	}
	if got, _ := os.ReadFile(target); string(got) != `{"ok": true}` {
		t.Fatalf("--output wrote %q", got)
	}
}
