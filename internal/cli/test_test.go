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
	// --output must never truncate a feature file, and is not created when the run fails early.
	feature := filepath.Join(dir, "features", "p.feature")
	if code, _, stderr := run("test", "-C", dir, "--env", "dev", "--output", feature); code != 2 || !strings.Contains(stderr, "would overwrite") {
		t.Fatalf("output onto a feature: code=%d stderr=%s", code, stderr)
	}
	if data, _ := os.ReadFile(feature); !strings.Contains(string(data), "Feature: p") {
		t.Fatalf("feature file was damaged: %q", data)
	}
	// Environment files, the project config and the session are inputs too.
	for _, name := range []string{"http-client.env.json", "apic.yaml", ".env", filepath.Join(".apic", "session.json")} {
		if code, _, stderr := run("test", "-C", dir, "--env", "dev", "--output", filepath.Join(dir, name)); code != 2 || !strings.Contains(stderr, "would overwrite") {
			t.Fatalf("output onto %s: code=%d stderr=%s", name, code, stderr)
		}
	}
	// A request body file is an input as well.
	must(t, os.WriteFile(filepath.Join(dir, "payload.json"), []byte(`{"a":1}`), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "post.http"), []byte("### b\n# @name post\nPOST {{baseUrl}}/p\nContent-Type: application/json\n\n< ./payload.json\n"), 0o644))
	if code, _, stderr := run("test", "-C", dir, "--env", "dev", "--output", filepath.Join(dir, "payload.json")); code != 2 || !strings.Contains(stderr, "would overwrite") {
		t.Fatalf("output onto a body file: code=%d stderr=%s", code, stderr)
	}
	// An explicitly selected feature under a dot directory, hard-linked to the output.
	must(t, os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, ".hidden", "h.feature"), []byte("Feature: h\n  Scenario: s\n    When I run \"ping\"\n"), 0o644))
	if err := os.Link(filepath.Join(dir, ".hidden", "h.feature"), filepath.Join(dir, "linked.xml")); err == nil {
		if code, _, stderr := run("test", "-C", dir, "--env", "dev", "--output", filepath.Join(dir, "linked.xml"), ".hidden/h.feature"); code != 2 || !strings.Contains(stderr, "would overwrite") {
			t.Fatalf("hard-linked output onto a selected feature: code=%d stderr=%s", code, stderr)
		}
		must(t, os.Remove(filepath.Join(dir, "linked.xml")))
	}
	// Outside the project the name heuristics do not apply.
	outside := filepath.Join(t.TempDir(), "report.http")
	if code, _, stderr := run("test", "-C", dir, "--env", "dev", "--output", outside); code != 3 || strings.Contains(stderr, "would overwrite") {
		t.Fatalf("output outside the project: code=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("report outside the project must be written: %v", err)
	}
	// A symlink alias of a feature is refused too.
	alias := filepath.Join(dir, "report.xml")
	if err := os.Symlink(feature, alias); err == nil {
		if code, _, stderr := run("test", "-C", dir, "--env", "dev", "--output", alias); code != 2 || !strings.Contains(stderr, "would overwrite") {
			t.Fatalf("symlinked output: code=%d stderr=%s", code, stderr)
		}
		must(t, os.Remove(alias))
	}
	if code, _, stderr := run("test", "-C", dir, "--env", "dev", "--use-session", "--no-session"); code != 2 || !strings.Contains(stderr, "cannot be combined") {
		t.Fatalf("conflicting session flags: code=%d stderr=%s", code, stderr)
	}
	// A report written to a file never contains colour escapes, whatever the flags.
	pretty := filepath.Join(dir, "pretty.txt")
	_, _, _ = run("test", "-C", dir, "--env", "dev", "--format", "pretty", "--output", pretty)
	if data, _ := os.ReadFile(pretty); len(data) == 0 || strings.Contains(string(data), "\x1b[") {
		t.Fatalf("file report must be plain text: %q", data)
	}
	report := filepath.Join(dir, "report.xml")
	if code, _, _ := run("test", "-C", dir, "--env", "nope", "--output", report); code != 2 {
		t.Fatalf("code=%d", code)
	}
	if _, err := os.Stat(report); !os.IsNotExist(err) {
		t.Fatal("report file should not be created when the run never starts")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestLazyFileReportsWriteErrorsOnClose(t *testing.T) {
	lf := &lazyFile{path: filepath.Join(t.TempDir(), "missing", "report.xml")}
	if _, err := lf.Write([]byte("x")); err == nil {
		t.Fatal("writing into a missing directory must fail")
	}
	if err := lf.Close(); err == nil {
		t.Fatal("Close must report the earlier write failure")
	}
	ok := &lazyFile{path: filepath.Join(t.TempDir(), "report.xml")}
	if _, err := ok.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := ok.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOutputOntoRequestFileOutsideRootIsRefused(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "proj")
	api := filepath.Join(base, "api")
	must(t, os.MkdirAll(filepath.Join(dir, "features"), 0o755))
	must(t, os.MkdirAll(api, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "apic.yaml"), []byte("dir: ../api\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(`{"dev":{"baseUrl":"http://127.0.0.1:1"}}`), 0o644))
	must(t, os.WriteFile(filepath.Join(api, "x.http"), []byte("### a\n# @name ping\nGET {{baseUrl}}/ping\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "features", "p.feature"), []byte("Feature: p\n  Scenario: s\n    When I run \"ping\"\n"), 0o644))
	alias := filepath.Join(dir, "report.xml")
	if err := os.Link(filepath.Join(api, "x.http"), alias); err != nil {
		t.Skip("hard links not supported here")
	}
	a := New()
	var out, errb bytes.Buffer
	a.Stdout, a.Stderr = &out, &errb
	if code := a.Execute(context.Background(), []string{"test", "-C", dir, "--env", "dev", "--output", alias}); code != 2 || !strings.Contains(errb.String(), "would overwrite") {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if data, _ := os.ReadFile(filepath.Join(api, "x.http")); !strings.Contains(string(data), "@name ping") {
		t.Fatalf("request file was damaged: %q", data)
	}
}

func TestRealPathResolvesMissingLeaves(t *testing.T) {
	dir := t.TempDir()
	real, err := filepath.EvalSymlinks(dir)
	must(t, err)
	got := realPath(filepath.Join(dir, "missing", "deeper", "report.xml"))
	if got != filepath.Join(real, "missing", "deeper", "report.xml") {
		t.Fatalf("got %s", got)
	}
	if got := realPath(dir); got != real {
		t.Fatalf("existing path: got %s want %s", got, real)
	}
}
