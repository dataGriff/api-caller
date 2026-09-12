package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/openapi"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) importCmd() *cobra.Command {
	var out, envName string
	var force bool
	cmd := &cobra.Command{
		Use:   "import <openapi.yaml|openapi.json>",
		Short: "Scaffold .http files from an OpenAPI 3 document",
		Long: `Generate one .http file per tag with a named request per operation. Path
parameters become {{variables}}, request bodies get an example built from
the schema, and http-client.env.json is created with the server URL.
Existing files are left alone unless --force is given.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := openapi.Import(args[0], openapi.Options{OutDir: out, EnvName: envName, Force: force})
			if err != nil {
				return &runner.UsageError{Msg: err.Error()}
			}
			if a.g.json {
				return a.writeJSON(res)
			}
			for _, f := range res.Files {
				fmt.Fprintf(a.Stdout, "wrote %s\n", f)
			}
			for _, f := range res.Skipped {
				fmt.Fprintf(a.Stdout, "kept  %s (use --force to overwrite)\n", f)
			}
			if res.EnvFile != "" {
				fmt.Fprintf(a.Stdout, "wrote %s (baseUrl = %s)\n", res.EnvFile, res.BaseURL)
			}
			fmt.Fprintf(a.Stdout, "%d request(s) generated; run `apic list` to see them\n", res.Requests)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", ".", "directory to write .http files into")
	cmd.Flags().StringVar(&envName, "env-name", "dev", "environment name for the generated http-client.env.json")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return cmd
}
