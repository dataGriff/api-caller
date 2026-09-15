package cli

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/demoapi"
	"github.com/dataGriff/api-caller/internal/runner"
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
  apic run login whoami -C apic-demo --env local
  apic ui -C apic-demo --env local

Every apic command, including ui, resolves .http files relative to -C
(default: the current directory) — apic ui on its own won't find the
project this command just wrote unless you cd into it or pass -C.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if port < 1 || port > 65535 {
				return &runner.UsageError{Msg: "--port must be between 1 and 65535"}
			}
			written, skipped, err := demoapi.WriteProject(out, port, force)
			if err != nil {
				return err
			}
			url := fmt.Sprintf("http://localhost:%d", port)
			if a.g.json {
				if err := a.writeJSON(struct {
					Out       string   `json:"out"`
					URL       string   `json:"url"`
					Written   []string `json:"written"`
					Skipped   []string `json:"skipped,omitempty"`
					Listening bool     `json:"listening"`
				}{
					Out:       out,
					URL:       url,
					Written:   written,
					Skipped:   skipped,
					Listening: true,
				}); err != nil {
					return err
				}
			} else {
				for _, f := range written {
					fmt.Fprintf(a.Stdout, "wrote %s\n", f)
				}
				for _, f := range skipped {
					fmt.Fprintf(a.Stdout, "kept  %s (use --force to overwrite)\n", f)
				}
				fmt.Fprintf(a.Stdout, "demo api listening on %s\n", url)
				fmt.Fprintf(a.Stdout, "try: apic run login whoami -C %s --env local\n", out)
			}
			addr := fmt.Sprintf("127.0.0.1:%d", port)
			return http.ListenAndServe(addr, demoapi.New())
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "apic-demo", "directory to write the example project into")
	cmd.Flags().IntVar(&port, "port", 8089, "port to serve the demo API on")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return cmd
}
