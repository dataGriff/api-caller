// apic describe: what a request needs and where each value comes from.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) describeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe <request>",
		Short: "Show a request's variables, where each comes from, captures and asserts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, req, err := a.single(args[0])
			if err != nil {
				return err
			}
			d := r.Describe(req)
			if a.g.json {
				return a.writeJSON(maskDescription(d))
			}
			fmt.Fprint(a.Stdout, output.Describe(theme, d, req.Headers))
			return nil
		},
	}
	cmd.ValidArgsFunction = a.completeRequests
	return cmd
}

func maskDescription(d *runner.Description) *runner.Description {
	out := *d
	out.Variables = append([]runner.VarInfo(nil), d.Variables...)
	for i := range out.Variables {
		if out.Variables[i].Secret {
			out.Variables[i].Value = "***"
		}
	}
	return &out
}
