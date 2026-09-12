package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateChecksAssertSelector(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api.http"), []byte(`
# @name t
# @assert bogus == 1
GET https://example.com
`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	diags := p.Validate()
	if len(diags) == 0 || diags[0].Severity != "error" || !strings.Contains(diags[0].Message, "unknown selector") {
		t.Fatalf("diagnostics: %+v", diags)
	}
}

func TestValidateWarnsWhenDefaultExecIsDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte("auth:\n  default: exec whoami\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	diags := p.Validate()
	if len(diags) == 0 || diags[0].Severity != "warning" || !strings.Contains(diags[0].Message, "auth.default: @auth exec will be refused") {
		t.Fatalf("diagnostics: %+v", diags)
	}
}
