// apic curl: a request as the curl command that sends the same thing.
// It is `apic snippet --lang curl`, kept for its own --json shape.

package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (a *App) curlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "curl <request>",
		Short: "Print the equivalent curl command (with variables resolved)",
		Long: `Print the curl command that sends what apic would, variables resolved.
It is apic snippet --lang curl; see apic snippet for other languages.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, command, err := a.snippet(args[0], "curl")
			if err != nil {
				return err
			}
			if a.g.json {
				return a.writeJSON(struct {
					ID      string `json:"id"`
					Command string `json:"command"`
				}{id, command})
			}
			fmt.Fprintln(a.Stdout, command)
			return nil
		},
	}
	cmd.ValidArgsFunction = a.completeRequests
	return cmd
}
