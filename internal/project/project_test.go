package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/httpfile"
)

func httpfileCodes() map[string]string { return httpfile.Codes }

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
	// The span covers just the selector, `bogus`, on `# @assert bogus == 1`.
	if d := diags[0]; d.Code != "unknown-selector" || d.Line != 3 || d.Column != 11 || d.EndLine != 3 || d.EndColumn != 16 {
		t.Fatalf("span/code: %+v", d)
	}
}

// TestValidateSpansAndCodes pins the code and span of each project-level
// check, since editors and the github/sarif formats key on them.
func TestValidateSpansAndCodes(t *testing.T) {
	dir := t.TempDir()
	src := "### a\n# @name dup\n# @step I run {x}\n# @auth bogus\n# @capture v = nope.$\nGET https://example.com\n\n### b\n# @name dup\n# @assert status =\nPOST https://example.com\n\n< ./missing.json\n\n### c\n# @name c\n# @ref nope\n# @ref d\nGET https://example.com\n\n### d\n# @name d\n# @ref c\n# @retry soon\nGET https://example.com\n\n### e\n# @name e\n# @ref e\nGET https://example.com\n"
	if err := os.WriteFile(filepath.Join(dir, "api.http"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	type span struct{ line, col, end int }
	want := map[string][]span{
		"duplicate-name":    {{2, 9, 12}, {9, 9, 12}},
		"ambiguous-step":    {{3, 9, 18}},
		"bad-auth":          {{4, 9, 14}},
		"unknown-selector":  {{5, 16, 22}},
		"bad-assert":        {{10, 11, 19}},
		"missing-body-file": {{13, 3, 17}},
		"bad-ref":           {{17, 8, 12}},
		"ref-cycle":         {{18, 8, 9}, {23, 8, 9}, {29, 8, 9}},
		"bad-retry":         {{24, 10, 14}},
	}
	got := map[string][]span{}
	for _, d := range p.Validate() {
		got[d.Code] = append(got[d.Code], span{d.Line, d.Column, d.EndColumn})
		if d.Column > 0 && d.EndLine != d.Line {
			t.Errorf("%s: end line %d != line %d", d.Code, d.EndLine, d.Line)
		}
	}
	for code, spans := range want {
		if len(got[code]) != len(spans) {
			t.Errorf("%s: got %v, want %v", code, got[code], spans)
			continue
		}
		for i := range spans {
			if got[code][i] != spans[i] {
				t.Errorf("%s[%d]: got %v, want %v", code, i, got[code][i], spans[i])
			}
		}
	}
	for code := range got {
		if _, ok := httpfileCodes()[code]; !ok {
			t.Errorf("code %q is not documented in httpfile.Codes", code)
		}
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
		if d.Severity == "error" && strings.Contains(d.Message, `@step "I do {y}" matches the same text as @step "I do {x}" on a (api.http:3)`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("equivalent phrases not reported: %v", p.Validate())
	}
}

func TestValidatePhraseConflictsWithBuiltin(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api.http"), []byte("### a\n# @name a\n# @step I run {thing}\nGET http://x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range p.Validate() {
		if d.Severity == "error" && strings.Contains(d.Message, "the built-in step run") {
			found = true
		}
	}
	if !found {
		t.Fatalf("built-in conflict not reported: %v", p.Validate())
	}
}

func TestValidateRejectsTooManyPhraseParams(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api.http"), []byte("### a\n# @name a\n# @step {a} {b} {c} {d} {e} {f} {g}\nGET http://x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range p.Validate() {
		if d.Severity == "error" && strings.Contains(d.Message, "more than 6 parameters") {
			found = true
		}
	}
	if !found {
		t.Fatalf("arity not reported: %v", p.Validate())
	}
}
