package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
