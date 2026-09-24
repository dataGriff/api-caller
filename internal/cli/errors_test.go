package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dataGriff/api-caller/internal/runner"
)

// errorCase makes apic fail with one catalogue code: files for a project,
// then the arguments (--json is added). setup, when set, starts what the
// case needs and returns replacements for {{url}} in the files.
type errorCase struct {
	files map[string]string
	args  []string
	setup func(t *testing.T) string
	// cancel, when set, cancels the command's context after this long.
	cancel time.Duration
}

func errorCases() map[runner.Code]errorCase {
	get := func(extra string) map[string]string {
		return map[string]string{"a.http": extra + "GET {{url}}/\n"}
	}
	hang := func(t *testing.T) string {
		done := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-done:
			case <-r.Context().Done():
			}
		}))
		t.Cleanup(func() { close(done); srv.Close() })
		return srv.URL
	}
	ok := func(t *testing.T) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", 100)))
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	return map[runner.Code]errorCase{
		runner.CodeMissingVariable: {files: map[string]string{"a.http": "GET http://localhost/{{token}}\n"}, args: []string{"run", "a.http"}},
		runner.CodeBuild:           {files: map[string]string{"a.http": "GET http://localhost/{{$randomInt 5 1}}\n"}, args: []string{"run", "a.http"}},
		runner.CodeInvalidFile:     {files: map[string]string{"a.http": "# @capture nonsense\nGET http://localhost/\n"}, args: []string{"run", "a.http"}},
		runner.CodeDirective:       {files: map[string]string{"a.http": "# @timeout soon\nGET http://localhost/\n"}, args: []string{"run", "a.http"}},
		runner.CodeAuth:            {files: map[string]string{"a.http": "# @auth nonsense\nGET http://localhost/\n"}, args: []string{"run", "a.http"}},
		runner.CodeBodyFile:        {files: map[string]string{"a.http": "POST http://localhost/\n\n< missing.json\n"}, args: []string{"run", "a.http"}},
		runner.CodeRef:             {files: map[string]string{"a.http": "# @ref nope\nGET http://localhost/{{token}}\n"}, args: []string{"run", "a.http"}},
		runner.CodeUnknownRequest:  {files: get(""), args: []string{"run", "nope"}},
		runner.CodeAmbiguous:       {files: map[string]string{"a.http": "GET http://localhost/1\n\n###\nGET http://localhost/2\n"}, args: []string{"describe", "a.http"}},
		runner.CodeFlag:            {files: get(""), args: []string{"run", "--bogus"}},
		runner.CodeEnvironment:     {files: get(""), args: []string{"run", "a.http", "--env", "nope"}},
		runner.CodeProject:         {files: map[string]string{"apic.yaml": "timeout: [\n", "a.http": "GET http://localhost/\n"}, args: []string{"run", "a.http"}},
		runner.CodeTLSConfig:       {files: map[string]string{"ca.pem": "not a certificate\n", "a.http": "GET https://localhost/\n"}, args: []string{"run", "a.http", "--cacert", "{{dir}}/ca.pem"}},
		runner.CodeProxy:           {files: get(""), args: []string{"run", "a.http", "--var", "url=http://localhost", "--proxy", "ftp://proxy"}},
		runner.CodeSession:         {files: get(""), args: []string{"session", "--no-session"}},
		runner.CodeData:            {files: map[string]string{"a.http": "GET http://localhost/{{id}}\n", "rows.csv": ""}, args: []string{"run", "a.http", "--data", "{{dir}}/rows.csv"}},
		runner.CodeFile:            {files: get(""), args: []string{"fmt", "{{dir}}/missing.http"}},
		runner.CodeImport:          {files: map[string]string{"spec.json": "{\"not\": \"a spec\"}"}, args: []string{"import", "{{dir}}/spec.json", "--out", "{{dir}}/out"}},
		runner.CodeFeatures:        {files: get(""), args: []string{"test"}},
		runner.CodeTerminal:        {files: get(""), args: []string{"ui"}},
		runner.CodeServer:          {files: get(""), args: []string{"mcp", "--http", "256.0.0.1:1"}},
		runner.CodeTransport: {files: get(""), args: []string{"run", "a.http"}, setup: func(t *testing.T) string {
			// A server that hangs up without answering: not a refused
			// connection, not a timeout, just a failed request.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}))
			t.Cleanup(srv.Close)
			return srv.URL
		}},
		runner.CodeConnect: {files: get(""), args: []string{"run", "a.http"}, setup: func(t *testing.T) string {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			addr := l.Addr().String()
			_ = l.Close() // nothing listens there now
			return "http://" + addr
		}},
		runner.CodeTimeout: {files: get(""), args: []string{"run", "a.http", "--timeout", "50ms"}, setup: hang},
		runner.CodeTLS: {files: get(""), args: []string{"run", "a.http"}, setup: func(t *testing.T) string {
			srv := httptest.NewTLSServer(http.NotFoundHandler())
			t.Cleanup(srv.Close)
			return srv.URL
		}},
		runner.CodeProtocol:  {files: map[string]string{"apic.yaml": "maxBodyBytes: 10\n", "a.http": "GET {{url}}/\n"}, args: []string{"run", "a.http"}, setup: ok},
		runner.CodeCancelled: {files: get(""), args: []string{"run", "a.http"}, setup: hang, cancel: 100 * time.Millisecond},
	}
}

// TestEveryCodeIsProduced runs a real command for each code in the
// catalogue and checks the --json error object on stderr. A new code
// without a case here fails the test.
func TestEveryCodeIsProduced(t *testing.T) {
	cases := errorCases()
	for _, e := range runner.Catalogue {
		if e.Code == runner.CodeOther {
			// Every error apic makes itself has a code; E200 is the
			// fallback for one that slipped through.
			if got := runner.CodeOf(errors.New("anything")); got != runner.CodeOther {
				t.Errorf("an uncoded error is %s", got)
			}
			continue
		}
		c, ok := cases[e.Code]
		if !ok {
			t.Errorf("%s (%s): no test produces it; add a case to errorCases", e.Code, e.Title)
			continue
		}
		t.Run(string(e.Code), func(t *testing.T) {
			dir := t.TempDir()
			url := "http://localhost"
			if c.setup != nil {
				url = c.setup(t)
			}
			for name, body := range c.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.ReplaceAll(body, "{{url}}", url)), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if c.cancel > 0 {
				time.AfterFunc(c.cancel, cancel)
			}
			a := New()
			var out, errb bytes.Buffer
			a.Stdout, a.Stderr = &out, &errb
			args := []string{"-C", dir, "--json"}
			for _, arg := range c.args {
				args = append(args, strings.ReplaceAll(arg, "{{dir}}", dir))
			}
			exit := a.Execute(ctx, args)
			var got struct {
				Error runner.ErrorInfo `json:"error"`
			}
			if err := json.Unmarshal(errb.Bytes(), &got); err != nil {
				t.Fatalf("stderr is not one JSON error object (exit %d): %v\n%s", exit, err, errb.String())
			}
			if got.Error.Code != e.Code || exit != e.Exit || got.Error.Exit != e.Exit {
				t.Fatalf("want %s exit %d, got %s exit %d (object says %d): %s", e.Code, e.Exit, got.Error.Code, exit, got.Error.Exit, got.Error.Message)
			}
			if got.Error.Title != e.Title || got.Error.Hint != e.Hint || got.Error.URL != e.Code.URL() || got.Error.Message == "" {
				t.Fatalf("incomplete error object: %+v", got.Error)
			}
		})
	}
}
