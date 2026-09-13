package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestCommandExitCodes(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "api.http"), []byte("### a\n# @name ping\nGET {{baseUrl}}/ping\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"http://127.0.0.1:1"}}`), 0o644))
	must(t, os.MkdirAll(filepath.Join(dir, "features"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "features", "p.feature"), []byte("Feature: p\n  Scenario: s\n    When I run \"ping\"\n"), 0o644))

	run := func(args ...string) (int, string, string) {
		a := New()
		var out, errb bytes.Buffer
		a.Stdout, a.Stderr = &out, &errb
		code := a.Execute(context.Background(), args)
		return code, out.String(), errb.String()
	}
	if code, _, stderr := run("test", "-C", dir, "--env", "dev", "--no-color"); code != 3 || !strings.Contains(stderr, "request failed") {
		t.Fatalf("unreachable server should exit 3: code=%d stderr=%s", code, stderr)
	}
	if code, _, stderr := run("test", "-C", dir, "--env", "nope"); code != 2 || !strings.Contains(stderr, `environment "nope"`) {
		t.Fatalf("unknown env should exit 2: code=%d stderr=%s", code, stderr)
	}
	if code, out, _ := run("test", "--steps", "--json", "-C", t.TempDir()); code != 0 || !strings.Contains(out, `"pattern"`) {
		t.Fatalf("--steps --json outside a project: code=%d out=%s", code, out)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
