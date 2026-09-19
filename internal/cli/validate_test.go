package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/httpfile"
)

// validateProject writes a project with one warning and one error and runs
// `apic validate` on it with the given extra flags.
func validateProject(t *testing.T, flags ...string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "### a\n# @name a\n# @frobnicate yes\n# @assert bogus == 1\nGET https://example.com\n")
	app := New()
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr
	args := append([]string{"validate", "-C", dir}, flags...)
	code := app.Execute(context.Background(), args)
	return code, stdout.String(), stderr.String()
}

func TestValidateTextShowsColumnsAndCodes(t *testing.T) {
	code, out, _ := validateProject(t)
	if code != 2 {
		t.Fatalf("code = %d, want 2:\n%s", code, out)
	}
	for _, want := range []string{
		"api.http:3:3: warning: unknown directive @frobnicate (ignored) (unknown-directive)",
		"api.http:4:11: error: assert \"bogus == 1\": unknown selector \"bogus\" (unknown-selector)",
		"1 file, 1 request, 1 error, 1 warning",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Stdout is a buffer, not a terminal, so no caret lines.
	if strings.Contains(out, "^") {
		t.Errorf("caret line printed without a terminal:\n%s", out)
	}
}

func TestValidateJSONCarriesSpans(t *testing.T) {
	code, out, _ := validateProject(t, "--json")
	if code != 2 {
		t.Fatalf("code = %d:\n%s", code, out)
	}
	var got struct {
		OK          bool `json:"ok"`
		Diagnostics []struct {
			Path      string `json:"path"`
			Line      int    `json:"line"`
			Column    int    `json:"column"`
			EndLine   int    `json:"end_line"`
			EndColumn int    `json:"end_column"`
			Severity  string `json:"severity"`
			Code      string `json:"code"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.OK || len(got.Diagnostics) != 2 {
		t.Fatalf("got %+v", got)
	}
	d := got.Diagnostics[1]
	if d.Path != "api.http" || d.Line != 4 || d.Column != 11 || d.EndLine != 4 || d.EndColumn != 16 || d.Code != "unknown-selector" || d.Severity != "error" {
		t.Fatalf("diagnostic = %+v", d)
	}
}

func TestValidateGitHubFormat(t *testing.T) {
	code, out, _ := validateProject(t, "--format", "github")
	if code != 2 {
		t.Fatalf("code = %d:\n%s", code, out)
	}
	for _, want := range []string{
		"::warning file=api.http,line=3,col=3,endLine=3,endColumn=14,title=apic%3A unknown-directive::unknown directive @frobnicate (ignored)",
		"::error file=api.http,line=4,col=11,endLine=4,endColumn=16,title=apic%3A unknown-selector::assert \"bogus == 1\": unknown selector \"bogus\"",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestValidateSARIFFormat(t *testing.T) {
	code, out, _ := validateProject(t, "--format", "sarif")
	if code != 2 {
		t.Fatalf("code = %d:\n%s", code, out)
	}
	var got struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				Level     string `json:"level"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine, StartColumn, EndLine, EndColumn int
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "2.1.0" || len(got.Runs) != 1 || got.Runs[0].Tool.Driver.Name != "apic" {
		t.Fatalf("header: %+v", got)
	}
	run := got.Runs[0]
	if len(run.Tool.Driver.Rules) != 2 || run.Tool.Driver.Rules[0].ID != "unknown-directive" || run.Tool.Driver.Rules[1].ID != "unknown-selector" {
		t.Fatalf("rules: %+v", run.Tool.Driver.Rules)
	}
	if len(run.Results) != 2 {
		t.Fatalf("results: %+v", run.Results)
	}
	r := run.Results[1]
	loc := r.Locations[0].PhysicalLocation
	if r.RuleID != "unknown-selector" || r.Level != "error" || loc.ArtifactLocation.URI != "api.http" ||
		loc.Region.StartLine != 4 || loc.Region.StartColumn != 11 || loc.Region.EndLine != 4 || loc.Region.EndColumn != 16 {
		t.Fatalf("result: %+v", r)
	}
}

func TestValidateRejectsUnknownFormat(t *testing.T) {
	code, _, stderr := validateProject(t, "--format", "xml")
	if code != 2 || !strings.Contains(stderr, "--format must be text, json, github or sarif") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestGitHubAnnotationEscapes(t *testing.T) {
	got := githubAnnotation(diagFor("a,b:c.http", "50% done\nnext"))
	want := "::error file=a%2Cb%3Ac.http,line=1,title=apic%3A t::50%25 done%0Anext"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestCaretLineUnderlinesTheSpan(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "# @assert bogus == 1\nGET https://x\n")
	d := diagFor("api.http", "m")
	d.Column, d.EndLine, d.EndColumn = 11, 1, 16
	got := caretLine(dir, d)
	if !strings.Contains(got, "# @assert bogus == 1") || !strings.HasSuffix(got, strings.Repeat(" ", 10)+"^^^^^\n") {
		t.Fatalf("got %q", got)
	}
	if caretLine(dir, diagFor("api.http", "m")) != "" {
		t.Fatal("a diagnostic without a span has no caret")
	}
}

func diagFor(path, msg string) httpfile.Diagnostic {
	return httpfile.Diagnostic{Path: path, Line: 1, Severity: "error", Code: "t", Message: msg}
}
