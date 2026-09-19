package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
)

// TestInitGitignoresEveryCredentialFile pins that `apic init` ignores each
// file apic itself treats as a secret source. .env was missing, so a project
// scaffolded by apic would happily commit it.
func TestInitGitignoresEveryCredentialFile(t *testing.T) {
	dir := t.TempDir()
	app := New()
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr
	if code := app.Execute(context.Background(), []string{"init", dir}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("init should write a .gitignore: %v", err)
	}
	got := string(data)
	for _, want := range []string{"http-client.private.env.json", ".env", ".apic/"} {
		if !strings.Contains(got, want) {
			t.Errorf("generated .gitignore is missing %q:\n%s", want, got)
		}
	}
}

// TestInitPointsAtTheSchema pins the modeline that gives editors completion
// and validation for apic.yaml, and that the file still parses as config.
func TestInitPointsAtTheSchema(t *testing.T) {
	dir := t.TempDir()
	app := New()
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr
	if code := app.Execute(context.Background(), []string{"init", dir, "--env", "qa"}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "apic.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# yaml-language-server: $schema=https://datagriff.github.io/api-caller/schemas/apic.schema.json\n") {
		t.Fatalf("apic.yaml should start with the schema modeline:\n%s", data)
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Config.Env != "qa" {
		t.Fatalf("env = %q, want qa", p.Config.Env)
	}
}
