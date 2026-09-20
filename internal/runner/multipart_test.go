package runner

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const uploadHTTP = `
### Upload a report with a title
# @name upload
# @assert status == 201
# @assert body.$.fields.title == Quarterly report for {{user}}
POST {{baseUrl}}/upload
Content-Type: multipart/form-data; boundary=WebAppBoundary

--WebAppBoundary
Content-Disposition: form-data; name="title"

Quarterly report for {{user}}
--WebAppBoundary
Content-Disposition: form-data; name="file"; filename="report.pdf"
Content-Type: application/pdf

< ./files/report.pdf
--WebAppBoundary
Content-Disposition: form-data; name="meta"; filename="meta.json"
Content-Type: application/json

<@ ./files/meta.json
--WebAppBoundary--

### A part that needs a value nobody has
# @name upload-missing
POST {{baseUrl}}/upload
Content-Type: multipart/form-data; boundary=b

--b
Content-Disposition: form-data; name="who"

{{nobody}}
--b--

### A part file outside the project
# @name upload-escape
POST {{baseUrl}}/upload
Content-Type: multipart/form-data; boundary=b

--b
Content-Disposition: form-data; name="f"; filename="x"

< ../../etc/passwd
--b--
`

// pdf is binary-looking content with a NUL, a CR LF pair and a line that
// looks like a delimiter, all of which must go over the wire untouched.
var pdf = []byte("%PDF-1.4\x00\r\n--NotTheBoundary\nlooks like one\n%%EOF")

func uploadServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength <= 0 {
			http.Error(w, "no Content-Length", http.StatusLengthRequired)
			return
		}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || params["boundary"] == "" {
			http.Error(w, "not multipart: "+err.Error(), http.StatusBadRequest)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		fields := map[string]string{}
		files := map[string]map[string]string{}
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			data, _ := io.ReadAll(p)
			if p.FileName() == "" {
				fields[p.FormName()] = string(data)
				continue
			}
			files[p.FormName()] = map[string]string{"filename": p.FileName(), "type": p.Header.Get("Content-Type"), "content": string(data)}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"fields": fields, "files": files})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestMultipartBodyIsAssembledFromPartsAndFiles(t *testing.T) {
	srv := uploadServer(t)
	dir := writeProject(t, map[string]string{
		"api.http":             uploadHTTP,
		"http-client.env.json": `{"dev": {"baseUrl": "` + srv.URL + `", "user": "alice"}}`,
		"files/meta.json":      `{"owner": "{{user}}"}`,
	})
	if err := os.WriteFile(filepath.Join(dir, "files", "report.pdf"), pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true})
	reqs, _ := r.Project.Resolve("upload")
	res, err := r.Run(context.Background(), reqs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("not ok: %+v", res)
	}
	var got struct {
		Fields map[string]string
		Files  map[string]map[string]string
	}
	if err := json.Unmarshal(res.Raw().Body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Fields["title"] != "Quarterly report for alice" {
		t.Errorf("text part rendered = %q", got.Fields["title"])
	}
	if f := got.Files["file"]; f["filename"] != "report.pdf" || f["type"] != "application/pdf" || f["content"] != string(pdf) {
		t.Errorf("file part = %q, want the bytes of report.pdf as they are", f)
	}
	if f := got.Files["meta"]; f["content"] != `{"owner": "alice"}` {
		t.Errorf("<@ part = %q, want the file rendered", f["content"])
	}
	// The request view summarises the body rather than dumping bytes.
	if res.Request.Body != "<multipart: 3 parts, 2 files>" {
		t.Errorf("summary = %q", res.Request.Body)
	}
	if len(res.Request.Parts) != 3 || res.Request.Parts[0].Value != "Quarterly report for alice" || res.Request.Parts[1].File != "files/report.pdf" || res.Request.Parts[1].Size != len(pdf) {
		t.Errorf("parts = %+v", res.Request.Parts)
	}
	data, _ := json.Marshal(res)
	var view struct {
		Request struct {
			Body string `json:"body"`
		} `json:"request"`
	}
	if err := json.Unmarshal(data, &view); err != nil || view.Request.Body != "<multipart: 3 parts, 2 files>" {
		t.Errorf("json request body = %q (%v)", view.Request.Body, err)
	}
	// Redacted, the summary is masked like any other body.
	res.Redact = true
	if b := res.Request.DisplayBody(true); b != Masked {
		t.Errorf("redacted body = %q", b)
	}
	if !strings.HasPrefix(string(res.Request.BodyBytes()), "--WebAppBoundary\r\nContent-Disposition: form-data; name=\"title\"\r\n\r\nQuarterly report for alice\r\n--WebAppBoundary\r\n") {
		t.Errorf("wire body = %q", res.Request.BodyBytes())
	}

	// A missing variable inside a part is the usual missing-variable error.
	reqs, _ = r.Project.Resolve("upload-missing")
	_, err = r.Run(context.Background(), reqs[0])
	if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "{{nobody}}") {
		t.Errorf("missing variable in a part: %v", err)
	}
	// A part file is confined to the project like a whole-body file.
	reqs, _ = r.Project.Resolve("upload-escape")
	_, err = r.Run(context.Background(), reqs[0])
	if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "outside project root") {
		t.Errorf("part file escape: %v", err)
	}
}

func TestMultipartBodyThatDoesNotParseIsAUsageError(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"api.http": "### x\n# @name x\nPOST https://example.com/upload\nContent-Type: multipart/form-data\n\n--a\nContent-Disposition: form-data; name=\"a\"\n\n1\n--a--\n",
	})
	r := newRunner(t, dir, Options{NoSession: true})
	_, err := r.Run(context.Background(), r.Project.Requests()[0])
	if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "boundary") {
		t.Fatalf("want a boundary error, got %v", err)
	}
}
