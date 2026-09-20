package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/openapi"
	"github.com/dataGriff/api-caller/internal/postman"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) importCmd() *cobra.Command {
	var out, envName string
	var force bool
	var postmanEnvs []string
	cmd := &cobra.Command{
		Use:   "import <openapi.yaml|openapi.json|collection.postman.json>",
		Short: "Scaffold .http files from an OpenAPI 3 document or a Postman collection",
		Long: `Generate .http files from an OpenAPI 3 document (one file per tag, a named
request per operation, example bodies from the schemas, an env file with the
server URL) or from a Postman v2.1 collection (folders become files, requests
become named requests, variables become env files, and the simple pm.test
checks become # @assert and # @capture lines). The format is detected from
the file. Existing files are left alone unless --force is given.`,
		Example: `  apic import openapi.yaml -o api --env-name dev
  apic import collection.postman.json -o api --postman-env staging.postman_environment.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0]) //nolint:gosec // the file the user named on the command line
			if err != nil {
				return &runner.UsageError{Msg: err.Error()}
			}
			if postman.LooksLikeCollection(data) {
				res, err := postman.Import(args[0], postman.Options{OutDir: out, EnvFiles: postmanEnvs, Force: force})
				if err != nil {
					return &runner.UsageError{Msg: err.Error()}
				}
				if a.g.json {
					return a.writeJSON(res)
				}
				a.printImport(res.Files, res.Skipped, res.EnvFile, res.BaseURL, res.Requests)
				for _, u := range res.Unsupported {
					fmt.Fprintf(a.Stdout, "note  %s: %s: %s\n", u.Request, u.What, u.Reason)
				}
				return nil
			}
			if postman.LooksLikeEnvironment(data) {
				return &runner.UsageError{Msg: args[0] + " is a Postman environment; pass the collection and give environments with --postman-env"}
			}
			if len(postmanEnvs) > 0 {
				return &runner.UsageError{Msg: "--postman-env goes with a Postman collection, not an OpenAPI document"}
			}
			res, err := openapi.Import(args[0], openapi.Options{OutDir: out, EnvName: envName, Force: force})
			if err != nil {
				return &runner.UsageError{Msg: err.Error()}
			}
			if a.g.json {
				return a.writeJSON(res)
			}
			a.printImport(res.Files, res.Skipped, res.EnvFile, res.BaseURL, res.Requests)
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", ".", "directory to write .http files into")
	cmd.Flags().StringVar(&envName, "env-name", "dev", "environment name for the generated http-client.env.json (OpenAPI)")
	cmd.Flags().StringArrayVar(&postmanEnvs, "postman-env", nil, "Postman environment export to turn into an environment (repeatable)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return cmd
}

func (a *App) printImport(files, skipped []string, envFile, baseURL string, requests int) {
	for _, f := range files {
		if f == envFile {
			continue
		}
		fmt.Fprintf(a.Stdout, "wrote %s\n", f)
	}
	for _, f := range skipped {
		fmt.Fprintf(a.Stdout, "kept  %s (use --force to overwrite)\n", f)
	}
	if envFile != "" {
		if baseURL != "" {
			fmt.Fprintf(a.Stdout, "wrote %s (baseUrl = %s)\n", envFile, baseURL)
		} else {
			fmt.Fprintf(a.Stdout, "wrote %s\n", envFile)
		}
	}
	fmt.Fprintf(a.Stdout, "%d request(s) generated; run `apic list` to see them\n", requests)
}
