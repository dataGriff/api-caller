package cli

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/report"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) runCmd() *cobra.Command {
	var verbose, bodyOnly, keepGoing, noRetry bool
	var retry, reportPath, outputPath string
	cmd := &cobra.Command{
		Use:   "run <request|file.http|file.http#name>...",
		Short: "Send one request, or every request in a file as a flow",
		Long: `Send requests and report status, timing, body, captures and assertions.

Targets:
  get-user             a request with "# @name get-user"
  users.http           every request in the file, in order (a flow)
  users.http#get-user  a named request in a specific file
  users.http#3         the third request in the file

Captured values (# @capture) are stored per environment in .apic/session.json
so a later invocation can use them. Use --no-session to disable. A request
that declares "# @ref login" runs login first when a value it needs is
missing; "# @forceRef login" runs it first every time. One that declares
"# @retry 10 2s" is re-sent until its assertions pass, up to 10 times, two
seconds apart; each failed attempt prints a line as it happens.`,
		Example: `  apic run login
  apic run get-user --env staging --var userId=42
  apic run smoke.http --json | jq .response.status
  apic run get-user --body-only | jq .email
  apic run smoke.http --keep-going --report report.html
  apic run daily-report --output reports/daily.csv`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.newRunner()
			if err != nil {
				return err
			}
			r.Opts.KeepGoing = keepGoing
			r.Opts.Retry, r.Opts.NoRetry = retry, noRetry
			if !a.g.json && !bodyOnly {
				r.Progress = func(p runner.Progress) { fmt.Fprint(a.Stdout, output.Attempt(output.Default(), p)) }
			}
			var reqs []*httpfile.Request
			for _, t := range args {
				rs, err := r.Project.Resolve(t)
				if err != nil {
					return &runner.UsageError{Msg: err.Error()}
				}
				reqs = append(reqs, rs...)
			}
			flow := len(reqs) > 1
			if reportPath != "" {
				if err := outputOverlapsSources(reportPath, r.Project, nil); err != nil {
					return err
				}
			}
			if outputPath != "" {
				if flow {
					return &runner.UsageError{Msg: fmt.Sprintf("--output saves one response; the targets name %d requests (add `>> file` lines to the requests instead)", len(reqs))}
				}
				if err := outputOverlapsSources(outputPath, r.Project, nil); err != nil {
					return err
				}
				r.Opts.Output = outputPath
			}
			started := time.Now()
			// Each result is printed as it lands, so a flow shows progress
			// (and the attempt lines of a retried request sit under the
			// right request) instead of everything at the end.
			var printErr error
			printed := 0
			r.OnResult = func(res *runner.Result, err error) {
				defer func() { printed++ }()
				switch {
				case a.g.json:
					if e := output.JSON(a.Stdout, res); e != nil && printErr == nil {
						printErr = e
					}
				case bodyOnly:
					output.Body(a.Stdout, res)
				default:
					if flow && printed > 0 {
						fmt.Fprintln(a.Stdout)
					}
					if res.Response == nil && err != nil {
						// The error is printed by Execute; show what ran first
						// and the request line for context.
						output.Deps(a.Stdout, res, verbose)
						fmt.Fprintf(a.Stdout, "%s %s\n", res.Request.Method, res.Request.DisplayURL(res.Redact))
						return
					}
					output.Human(a.Stdout, res, verbose)
				}
			}
			results, runErr := r.RunAll(cmd.Context(), reqs)
			if printErr != nil {
				return printErr
			}
			failed := false
			for _, res := range results {
				if !res.OK {
					failed = true
				}
			}
			if flow && !a.g.json && !bodyOnly {
				output.Summary(a.Stdout, results)
			}
			if reportPath != "" {
				var buf bytes.Buffer
				meta := report.Meta{Version: Version, Env: r.Opts.Env, Time: started, Redacted: a.g.redact, Project: r.Project.Root}
				if err := report.Run(&buf, meta, results); err != nil {
					return &runner.UsageError{Msg: fmt.Sprintf("write report %s: %v", reportPath, err)}
				}
				if err := os.WriteFile(reportPath, buf.Bytes(), 0o644); err != nil { //nolint:gosec // a report the user asked for, at the path they named
					return &runner.UsageError{Msg: fmt.Sprintf("write report %s: %v (run result: %v)", reportPath, err, describeOutcome(runErr))}
				}
			}
			if runErr != nil {
				return runErr
			}
			if failed {
				return &exitError{code: runner.ExitAssert}
			}
			return nil
		},
	}
	cmd.ValidArgsFunction = a.completeRequests
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show request and response headers")
	cmd.Flags().BoolVar(&bodyOnly, "body-only", false, "print only the response body (for piping)")
	cmd.Flags().BoolVar(&keepGoing, "keep-going", false, "in a flow, continue after a failure")
	cmd.Flags().StringVar(&retry, "retry", "", "re-send until the assertions pass: \"<attempts> [interval]\", e.g. \"10 2s\" (requests with # @retry keep their own)")
	cmd.Flags().BoolVar(&noRetry, "no-retry", false, "send every request once, ignoring # @retry, --retry and apic.yaml")
	cmd.Flags().StringVar(&reportPath, "report", "", "also write a self-contained HTML report of the run to this file")
	cmd.Flags().StringVar(&outputPath, "output", "", "save the response body to this file (one request only; like a \">>! file\" line in the request)")
	return cmd
}
