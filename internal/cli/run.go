package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) runCmd() *cobra.Command {
	var verbose, bodyOnly, keepGoing bool
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
so a later invocation can use them. Use --no-session to disable.`,
		Example: `  apic run login
  apic run get-user --env staging --var userId=42
  apic run smoke.http --json | jq .response.status
  apic run get-user --body-only | jq .email`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.newRunner()
			if err != nil {
				return err
			}
			r.Opts.KeepGoing = keepGoing
			var reqs []*httpfile.Request
			for _, t := range args {
				rs, err := r.Project.Resolve(t)
				if err != nil {
					return &runner.UsageError{Msg: err.Error()}
				}
				reqs = append(reqs, rs...)
			}
			flow := len(reqs) > 1
			results, runErr := r.RunAll(cmd.Context(), reqs)
			failed := false
			for i, res := range results {
				if !res.OK {
					failed = true
				}
				switch {
				case a.g.json:
					if err := output.JSON(a.Stdout, res); err != nil {
						return err
					}
				case bodyOnly:
					output.Body(a.Stdout, res)
				default:
					if flow && i > 0 {
						fmt.Fprintln(a.Stdout)
					}
					if res.Response == nil && runErr != nil && i == len(results)-1 {
						// The error is printed by Execute; show the request line for context.
						fmt.Fprintf(a.Stdout, "%s %s\n", res.Request.Method, res.Request.URL)
						continue
					}
					output.Human(a.Stdout, res, verbose)
				}
			}
			if flow && !a.g.json && !bodyOnly {
				output.Summary(a.Stdout, results)
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
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show request and response headers")
	cmd.Flags().BoolVar(&bodyOnly, "body-only", false, "print only the response body (for piping)")
	cmd.Flags().BoolVar(&keepGoing, "keep-going", false, "in a flow, continue after a failure")
	return cmd
}
