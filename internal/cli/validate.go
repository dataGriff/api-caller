// apic validate: problems in the project, as text, JSON, GitHub annotations or SARIF.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) validateCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Parse every .http file and report problems (for CI)",
		Long: `validate parses every .http file, checks selectors, assertions, auth specs,
step phrases, body files and request names, and reports what it finds.
Errors exit 2, so it works as a pull-request gate.

--format picks how diagnostics are printed:
  text    path:line:col: severity: message (code), with the source line
          and a caret under the span when stdout is a terminal
  json    {"ok", "files", "requests", "diagnostics": [...]} (also --json)
  github  GitHub Actions workflow commands, so a failing check annotates
          the pull request at the right line
  sarif   SARIF 2.1.0, for code scanning uploads`,
		Example: `  apic validate
  apic validate --format github     # in a GitHub Actions step
  apic validate --format sarif > apic.sarif
  apic validate --json | jq '.diagnostics[] | select(.severity == "error")'`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.g.json {
				format = "json"
			}
			p, err := a.loadProject()
			if err != nil {
				return err
			}
			diags := p.Validate()
			if diags == nil {
				diags = []httpfile.Diagnostic{}
			}
			errs := 0
			for _, d := range diags {
				if d.Severity == "error" {
					errs++
				}
			}
			switch format {
			case "json":
				if err := a.writeJSON(struct {
					OK          bool                  `json:"ok"`
					Files       int                   `json:"files"`
					Requests    int                   `json:"requests"`
					Diagnostics []httpfile.Diagnostic `json:"diagnostics"`
				}{errs == 0, len(p.Files), len(p.Requests()), diags}); err != nil {
					return err
				}
			case "github":
				for _, d := range diags {
					fmt.Fprintln(a.Stdout, githubAnnotation(d))
				}
				a.validateSummary(p, diags, errs)
			case "sarif":
				if err := a.writeJSON(sarifReport(diags)); err != nil {
					return err
				}
			case "text":
				caret := isTerminal(a.Stdout)
				for _, d := range diags {
					sev := theme.Warn.Render(d.Severity)
					if d.Severity == "error" {
						sev = theme.Fail.Render(d.Severity)
					}
					where := fmt.Sprintf("%s:%d", d.Path, d.Line)
					if d.Column > 0 {
						where += fmt.Sprintf(":%d", d.Column)
					}
					code := ""
					if d.Code != "" {
						code = " " + theme.Dim.Render("("+d.Code+")")
					}
					fmt.Fprintf(a.Stdout, "%s: %s: %s%s\n", where, sev, d.Message, code)
					if caret {
						fmt.Fprint(a.Stdout, caretLine(p.Root, d))
					}
				}
				a.validateSummary(p, diags, errs)
			default:
				return &runner.UsageError{Msg: fmt.Sprintf("--format must be text, json, github or sarif, got %q", format)}
			}
			if errs > 0 {
				return &exitError{code: runner.ExitUsage}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "text", "output format: text, json, github or sarif")
	return cmd
}

func (a *App) validateSummary(p *project.Project, diags []httpfile.Diagnostic, errs int) {
	counts := fmt.Sprintf("%s, %s", plural(len(p.Files), "file"), plural(len(p.Requests()), "request"))
	switch {
	case len(diags) == 0:
		fmt.Fprintf(a.Stdout, "%s %s, no problems\n", theme.OK.Render("✓"), counts)
	case errs == 0:
		fmt.Fprintf(a.Stdout, "%s %s, %s\n", theme.Warn.Render("!"), counts, plural(len(diags), "warning"))
	default:
		fmt.Fprintf(a.Stdout, "%s %s, %s, %s\n", theme.Fail.Render("✗"), counts, plural(errs, "error"), plural(len(diags)-errs, "warning"))
	}
}

// caretLine renders the source line a diagnostic points at with a caret
// under its span, for terminals. It returns "" when the file cannot be read
// or the diagnostic has no span.
func caretLine(root string, d httpfile.Diagnostic) string {
	if d.Column == 0 || d.Line == 0 {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(d.Path))) //nolint:gosec // the project's own file, named by validate
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if d.Line > len(lines) {
		return ""
	}
	src := lines[d.Line-1]
	width := d.EndColumn - d.Column
	if d.EndLine != d.Line || width < 1 || d.Column-1 > len(src) {
		width = 1
	}
	pad := strings.Repeat(" ", d.Column-1)
	return fmt.Sprintf("  %s\n  %s%s\n", theme.Dim.Render(src), pad, theme.Accent.Render(strings.Repeat("^", width)))
}

// githubAnnotation renders a diagnostic as a GitHub Actions workflow
// command, which the runner turns into an annotation on the pull request.
func githubAnnotation(d httpfile.Diagnostic) string {
	kind := "warning"
	if d.Severity == "error" {
		kind = "error"
	}
	props := []string{"file=" + ghEscapeProp(d.Path)}
	if d.Line > 0 {
		props = append(props, fmt.Sprintf("line=%d", d.Line))
		if d.Column > 0 {
			props = append(props, fmt.Sprintf("col=%d", d.Column))
			if d.EndLine >= d.Line && d.EndColumn > 0 {
				props = append(props, fmt.Sprintf("endLine=%d", d.EndLine), fmt.Sprintf("endColumn=%d", d.EndColumn))
			}
		}
	}
	if d.Code != "" {
		props = append(props, "title="+ghEscapeProp("apic: "+d.Code))
	}
	return fmt.Sprintf("::%s %s::%s", kind, strings.Join(props, ","), ghEscapeData(d.Message))
}

// The escaping rules for workflow commands: data escapes %, \r and \n;
// properties additionally escape : and ,.
func ghEscapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

func ghEscapeProp(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}

// sarifReport renders diagnostics as a SARIF 2.1.0 log with one rule per
// code, the shape GitHub code scanning and most IDEs accept.
func sarifReport(diags []httpfile.Diagnostic) map[string]any {
	codes := map[string]bool{}
	for _, d := range diags {
		if d.Code != "" {
			codes[d.Code] = true
		}
	}
	var rules []map[string]any
	names := make([]string, 0, len(codes))
	for c := range codes {
		names = append(names, c)
	}
	sort.Strings(names)
	for _, c := range names {
		rules = append(rules, map[string]any{
			"id":               c,
			"shortDescription": map[string]string{"text": httpfile.Codes[c]},
			"helpUri":          "https://datagriff.github.io/api-caller/cli/#apic-validate",
		})
	}
	if rules == nil {
		rules = []map[string]any{}
	}
	results := make([]map[string]any, 0, len(diags))
	for _, d := range diags {
		level := "warning"
		if d.Severity == "error" {
			level = "error"
		}
		region := map[string]any{}
		if d.Line > 0 {
			region["startLine"] = d.Line
			if d.Column > 0 {
				region["startColumn"] = d.Column
				if d.EndLine >= d.Line && d.EndColumn > 0 {
					region["endLine"] = d.EndLine
					region["endColumn"] = d.EndColumn
				}
			}
		} else {
			region["startLine"] = 1
		}
		res := map[string]any{
			"level":   level,
			"message": map[string]string{"text": d.Message},
			"locations": []map[string]any{{
				"physicalLocation": map[string]any{
					"artifactLocation": map[string]any{"uri": d.Path, "uriBaseId": "%SRCROOT%"},
					"region":           region,
				},
			}},
		}
		if d.Code != "" {
			res["ruleId"] = d.Code
		}
		results = append(results, res)
	}
	return map[string]any{
		"$schema": "https://json.schemastore.org/sarif-2.1.0.json",
		"version": "2.1.0",
		"runs": []map[string]any{{
			"tool": map[string]any{"driver": map[string]any{
				"name":           "apic",
				"version":        Version,
				"informationUri": "https://datagriff.github.io/api-caller/",
				"rules":          rules,
			}},
			"results": results,
		}},
	}
}
