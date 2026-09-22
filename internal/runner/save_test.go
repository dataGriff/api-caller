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

### A secret in the body only
# @name bodysecret
POST ` + srv.URL + `/data
Content-Type: application/json

{"key": "{{apiKey}}"}

>>! ../out/body.json

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
	if res.Response.BodyEncoding != Base64 {
		t.Fatal("a PNG is binary")
	}
	// Under --redact the body is the mask, which is text, so no encoding.
	res.Redact = true
	if d := res.DisplayResponse(); d.Body != Masked || d.BodyEncoding != "" {
		t.Fatalf("redacted: %v %q", d.Body, d.BodyEncoding)
	}
	res.Redact = false
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
	// A secret placeholder in the body tightens the file too.
	res, err = run("bodysecret")
	if err != nil || !res.OK || res.SavedTo != "out/body.json" {
		t.Fatalf("bodysecret: %v %+v", err, res)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(filepath.Join(dir, "out", "body.json")); st.Mode().Perm() != 0o600 {
			t.Errorf("a secret went into the body: mode %o", st.Mode().Perm())
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
	if diags := p.Validate(); len(diags) != 1 || diags[0].Code != "bad-save-path" || diags[0].Line != 34 || diags[0].Column != 4 {
		t.Fatalf("validate: %+v", diags)
	}
	d := r.Describe(p.Requests()[2])
	if d.SaveTo != "../out/data.json (overwrite)" {
		t.Fatalf("describe: %q", d.SaveTo)
	}

	// --output: a path from the working directory, overwriting, and in
	// place of the request's own `>>` line, so "again" (whose target
	// exists) succeeds and writes only there.
	target := filepath.Join(t.TempDir(), "deep", "logo.png")
	r.Opts.Output = target
	before, _ := os.Stat(filepath.Join(dir, "out", "logo.png"))
	res, err = run("again")
	if err != nil || !res.OK || res.SavedTo != target || len(res.Errors) != 0 {
		t.Fatalf("--output: %v %+v", err, res)
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, pngish) {
		t.Fatalf("--output wrote %d bytes", len(got))
	}
	if after, _ := os.Stat(filepath.Join(dir, "out", "logo.png")); after.ModTime() != before.ModTime() {
		t.Fatal("the request's own >> target was touched")
	}
}

// A project reached through a symlink (macOS keeps temp dirs under /var,
// which is /private/var) still saves into directories that do not exist
// yet: the confinement resolves the nearest existing ancestor.
func TestSaveResponseThroughSymlinkedRoot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("csv,data\n"))
	}))
	t.Cleanup(srv.Close)
	// The project is a real directory whose path goes through a symlink,
	// as /var/folders/... does on macOS.
	realParent := t.TempDir()
	linkParent := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realParent, linkParent); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	real := filepath.Join(realParent, "proj")
	link := filepath.Join(linkParent, "proj")
	if err := os.Mkdir(real, 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "api.http"), []byte("### r\n# @name r\nGET "+srv.URL+"/x\n\n>> ./new/deeper/out.csv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(link)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(p, Options{NoSession: true})
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(context.Background(), p.Requests()[0])
	if err != nil || !res.OK || res.SavedTo != "new/deeper/out.csv" {
		t.Fatalf("%v %+v", err, res)
	}
	if got, err := os.ReadFile(filepath.Join(real, "new", "deeper", "out.csv")); err != nil || string(got) != "csv,data\n" {
		t.Fatalf("%q %v", got, err)
	}
	// And an escape through the link is still an escape.
	if err := os.WriteFile(filepath.Join(real, "api.http"), []byte("### r\n# @name r\nGET "+srv.URL+"/x\n\n>> ../../nope/out.csv\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _ = project.Load(link)
	r, _ = New(p, Options{NoSession: true})
	res, err = r.Run(context.Background(), p.Requests()[0])
	if err != nil || res.OK || !strings.Contains(res.Errors[0], "outside project root") {
		t.Fatalf("%v %+v", err, res)
	}
}
