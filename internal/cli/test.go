package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/bdd"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) testCmd() *cobra.Command {
	var format, output, tags string
	var stopOnFailure, useSession, listSteps bool
	cmd := &cobra.Command{
		Use:   "test [path|file.feature]...",
		Short: "Run Gherkin feature files against the project's requests",
		Long: `Run .feature files with a built-in step vocabulary, no Cucumber runtime
needed. Requests are referenced by id ("When I run \"get-user\"") or through
phrases declared on them with "# @step" in the .http file.

Each scenario starts with a fresh, in-memory session so tests never touch
.apic/session.json (use --use-session to change that). Undefined steps fail
the run.

Exit codes: 0 all scenarios passed · 1 failures · 2 no features, unknown
environment or bad phrase.`,
		Example: `  apic test
  apic test features/users.feature --env staging --tags @smoke
  apic test --format junit --output report.xml
  apic test --json | jq '.[].elements[].steps[].result.status'
  apic test --steps`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if listSteps {
				return a.printSteps()
			}
			p, err := a.loadProject()
			if err != nil {
				return err
			}
			vars, err := a.varMap()
			if err != nil {
				return err
			}
			opts := bdd.Options{
				Config: bdd.Config{Project: p, Env: a.g.env, Vars: vars, UseSession: useSession,
					Timeout: a.g.timeout, Insecure: a.g.insecure, Redact: a.g.redact, Stderr: a.Stderr},
				Paths: args, Format: format, Tags: tags, StopOnFailure: stopOnFailure,
				NoColors: a.g.noColor || a.g.json || os.Getenv("NO_COLOR") != "",
				Output:   a.Stdout,
			}
			if opts.Env == "" {
				opts.Env = p.Config.Env
			}
			if a.g.json {
				opts.Format = "cucumber"
			}
			if output != "" {
				f, err := os.Create(output)
				if err != nil {
					return &runner.UsageError{Msg: err.Error()}
				}
				defer func() { _ = f.Close() }()
				opts.Output = f
			}
			code, err := bdd.Run(cmd.Context(), opts)
			if err != nil {
				return &runner.UsageError{Msg: err.Error()}
			}
			if code != 0 {
				return &exitError{code: code}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "pretty", "report format: "+strings.Join(bdd.Formats, ", "))
	cmd.Flags().StringVarP(&output, "output", "o", "", "write the report to a file instead of stdout")
	cmd.Flags().StringVarP(&tags, "tags", "t", "", `tag expression, e.g. "@smoke && ~@slow"`)
	cmd.Flags().BoolVar(&stopOnFailure, "stop-on-failure", false, "stop after the first failed scenario")
	cmd.Flags().BoolVar(&useSession, "use-session", false, "read and write .apic/session.json instead of an isolated session per scenario")
	cmd.Flags().BoolVar(&listSteps, "steps", false, "print the built-in step vocabulary and declared phrases, then exit")
	return cmd
}

func (a *App) printSteps() error {
	if a.g.json {
		p, err := a.loadProject()
		if err != nil {
			return err
		}
		type entry struct {
			Pattern string `json:"pattern"`
			Purpose string `json:"purpose"`
			Request string `json:"request,omitempty"`
		}
		var out []entry
		for _, v := range bdd.Vocabulary {
			out = append(out, entry{Pattern: v.Pattern, Purpose: v.Purpose})
		}
		for _, r := range p.Requests() {
			for _, st := range r.Steps() {
				out = append(out, entry{Pattern: st, Purpose: "runs " + r.ID(), Request: r.ID()})
			}
		}
		return a.writeJSON(out)
	}
	fmt.Fprintln(a.Stdout, styleBold.Render("built-in steps"))
	for _, v := range bdd.Vocabulary {
		fmt.Fprintf(a.Stdout, "  %s\n      %s\n", v.Pattern, styleDim.Render(v.Purpose))
	}
	p, err := a.loadProject()
	if err != nil {
		return nil // vocabulary alone is still useful outside a project
	}
	printed := false
	for _, r := range p.Requests() {
		for _, st := range r.Steps() {
			if !printed {
				fmt.Fprintln(a.Stdout, styleBold.Render("\nphrases declared in this project"))
				printed = true
			}
			fmt.Fprintf(a.Stdout, "  %s\n      %s\n", st, styleDim.Render("runs "+r.ID()+" ("+r.File.Path+")"))
		}
	}
	return nil
}
