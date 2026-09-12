package cli

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/demoapi"
)

func (a *App) demoCmd() *cobra.Command {
	var out string
	var port int
	var force bool
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Scaffold and serve a fake API, so apic can be tried with no setup",
		Long: `demo writes a small example .http project (login, whoami, and a todos
CRUD resource) into --out, then serves the fake API those requests target.
No network access or git clone needed. Existing files are left alone
unless --force is given.

Run it, then in another terminal (substituting your --out if you set one):
  apic run login whoami -C apic-demo --env local`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			written, skipped, err := demoapi.WriteProject(out, port, force)
			if err != nil {
				return err
			}
			for _, f := range written {
				fmt.Fprintf(a.Stdout, "wrote %s\n", f)
			}
			for _, f := range skipped {
				fmt.Fprintf(a.Stdout, "kept  %s (use --force to overwrite)\n", f)
			}
			addr := fmt.Sprintf(":%d", port)
			fmt.Fprintf(a.Stdout, "demo api listening on http://localhost%s\n", addr)
			fmt.Fprintf(a.Stdout, "try: apic run login whoami -C %s --env local\n", out)
			return http.ListenAndServe(addr, demoapi.New())
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "apic-demo", "directory to write the example project into")
	cmd.Flags().IntVar(&port, "port", 8089, "port to serve the demo API on")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return cmd
}
