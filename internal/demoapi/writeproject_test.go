package demoapi

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
)

func TestWriteProject(t *testing.T) {
	dir := t.TempDir()

	written, skipped, err := WriteProject(dir, 9999, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("first write should skip nothing, got %v", skipped)
	}
	wantFiles := []string{"apic.yaml", "http-client.env.json", "http-client.private.env.json", "auth.http", "todos.http", filepath.Join("features", "todos.feature")}
	if len(written) != len(wantFiles) {
		t.Fatalf("wrote %v, want %d files", written, len(wantFiles))
	}

	env, err := os.ReadFile(filepath.Join(dir, "http-client.env.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "http://localhost:9999") {
		t.Fatalf("http-client.env.json missing the requested port: %s", env)
	}
	info, err := os.Stat(filepath.Join(dir, "http-client.private.env.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no POSIX permission bits; Go reports 0666 there.
	if got := info.Mode().Perm(); got != 0o600 && runtime.GOOS != "windows" {
		t.Fatalf("http-client.private.env.json mode = %o, want 600", got)
	}

	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if diags := p.Validate(); len(diags) != 0 {
		t.Fatalf("scaffolded project has diagnostics: %+v", diags)
	}

	// Re-running without force leaves existing files alone.
	written, skipped, err = WriteProject(dir, 9999, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 0 || len(skipped) != len(wantFiles) {
		t.Fatalf("without force: written=%v skipped=%v", written, skipped)
	}

	// With force, every file is rewritten.
	written, skipped, err = WriteProject(dir, 9999, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 || len(written) != len(wantFiles) {
		t.Fatalf("with force: written=%v skipped=%v", written, skipped)
	}
}
