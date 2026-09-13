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

func TestLoadSkipsTestdataDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "testdata", "broken.http"), []byte(`
# @name t
# @capture nope
GET https://example.com
`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 0 || len(p.Diagnostics) != 0 {
		t.Fatalf("expected testdata dir to be skipped, got files=%+v diagnostics=%+v", p.Files, p.Diagnostics)
	}
}

func TestValidateDuplicatePhraseOnSameRequest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api.http"), []byte("### a\n# @name a\n# @step I do it\n# @step I do it\nGET http://x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range p.Validate() {
		if d.Severity == "error" && strings.Contains(d.Message, `@step "I do it" matches the same text as @step "I do it"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("duplicate phrase on the same request not reported: %v", p.Validate())
	}
}

func TestValidateDuplicatePhraseByMatcher(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api.http"), []byte("### a\n# @name a\n# @step I do {x}\nGET http://x\n\n### b\n# @name b\n# @step I do {y}\nGET http://y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range p.Validate() {
		if d.Severity == "error" && strings.Contains(d.Message, `@step "I do {y}" matches the same text as @step "I do {x}"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("equivalent phrases not reported: %v", p.Validate())
	}
}
