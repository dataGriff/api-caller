// apic curl: a request as the curl command that sends the same thing.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/curlexport"
)

func (a *App) curlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "curl <request>",
		Short: "Print the equivalent curl command (with variables resolved)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, req, err := a.single(args[0])
			if err != nil {
				return err
			}
			res, err := r.Resolve(req)
			if err != nil {
				return err
			}
			if d := r.Describe(req); !d.Ready {
				var missing []string
				for _, v := range d.Variables {
					if v.Missing {
						missing = append(missing, v.Name)
					}
				}
				return r.MissingError(req, missing)
			}
			command := curlexport.Command(res, a.g.redact)
			if a.g.json {
				return a.writeJSON(struct {
					ID      string `json:"id"`
					Command string `json:"command"`
				}{req.ID(), command})
			}
			fmt.Fprintln(a.Stdout, command)
			return nil
		},
	}
	cmd.ValidArgsFunction = a.completeRequests
	return cmd
}
