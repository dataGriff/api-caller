package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dataProject serves /echo/<id>, answering with the id and the X-Prev
// header it was sent, and 404 for the id "bad". The request captures the
// id as prev, so X-Prev shows whether a capture crossed iterations.
func dataProject(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/echo/")
		if id == "bad" {
			w.WriteHeader(404)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "prev": r.Header.Get("X-Prev")})
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "### echo\n# @name echo\n# @assert status == 200\n# @capture prev = body.$.id\nGET {{baseUrl}}/echo/{{id}}\nX-Prev: {{prev}}\n")
	mustWrite(t, filepath.Join(dir, "http-client.env.json"), `{"dev":{"baseUrl":"`+srv.URL+`","prev":"none"}}`)
	return dir
}

func jsonLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var objs []map[string]any
	dec := json.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var o map[string]any
		if err := dec.Decode(&o); err != nil {
			t.Fatalf("%v in %s", err, out)
		}
		objs = append(objs, o)
	}
	return objs
}

func TestRunData(t *testing.T) {
	dir := dataProject(t)
	rows := filepath.Join(dir, "rows.csv")
	mustWrite(t, rows, "id,note\n1,first\n2,second\n3,third\n")
	args := func(extra ...string) []string {
		return append([]string{"run", "echo", "-C", dir, "--env", "dev", "--no-color", "--data", rows}, extra...)
	}

	code, out, errb := execute(t, args()...)
	if code != 0 || !strings.Contains(out, "iteration 1/3 · id=1 note=first") || !strings.Contains(out, "iteration 3/3") || !strings.Contains(out, "3 of 3 iterations: 3 passed") {
		t.Fatalf("text: code=%d out=%s err=%s", code, out, errb)
	}
	// Iterations are isolated: no capture crosses, and the session file
	// is not written.
	code, out, _ = execute(t, args("--json")...)
	objs := jsonLines(t, out)
	if code != 0 || len(objs) != 3 {
		t.Fatalf("json: code=%d %s", code, out)
	}
	for i, o := range objs {
		it := o["iteration"].(map[string]any)
		body := o["response"].(map[string]any)["body"].(map[string]any)
		if it["index"] != float64(i+1) || it["total"] != float64(3) || it["row"].(map[string]any)["id"] != body["id"] || body["prev"] != "none" {
			t.Fatalf("iteration %d: %v %v", i+1, it, body)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".apic", "session.json")); err == nil {
		t.Fatal("isolated iterations must not write the session")
	}
	// Shared: each iteration sees the one before.
	code, out, _ = execute(t, args("--json", "--data-share-session")...)
	objs = jsonLines(t, out)
	if code != 0 || objs[1]["response"].(map[string]any)["body"].(map[string]any)["prev"] != "1" {
		t.Fatalf("shared: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".apic", "session.json")); err != nil {
		t.Fatal("a shared session is written as usual")
	}

	// A failing row stops the run, unless --keep-going.
	mustWrite(t, rows, "id\n1\nbad\n3\n")
	code, out, _ = execute(t, args()...)
	if code != 1 || strings.Contains(out, "iteration 3/3") || !strings.Contains(out, "2 of 3 iterations: 1 failed, 1 passed") {
		t.Fatalf("stop: code=%d %s", code, out)
	}
	code, out, _ = execute(t, args("--keep-going")...)
	if code != 1 || !strings.Contains(out, "3 of 3 iterations: 1 failed, 2 passed") {
		t.Fatalf("keep going: code=%d %s", code, out)
	}

	// JSON rows from stdin; --redact masks the row.
	a := New()
	var stdout, stderr bytes.Buffer
	a.Stdout, a.Stderr, a.Stdin = &stdout, &stderr, strings.NewReader(`[{"id": 42}]`)
	if code := a.Execute(context.Background(), []string{"run", "echo", "-C", dir, "--env", "dev", "--data", "-", "--json", "--redact"}); code != 0 {
		t.Fatalf("stdin: code=%d %s %s", code, stdout.String(), stderr.String())
	}
	if it := jsonLines(t, stdout.String())[0]["iteration"].(map[string]any); it["row"].(map[string]any)["id"] != "***" {
		t.Fatalf("redacted row: %v", it)
	}

	for _, bad := range [][]string{args("--output", filepath.Join(dir, "out.json")), {"run", "echo", "-C", dir, "--data", filepath.Join(dir, "missing.csv")}} {
		if code, _, errb := execute(t, bad...); code != 2 || !strings.Contains(errb, "--") {
			t.Fatalf("%v: code=%d %s", bad, code, errb)
		}
	}
}
