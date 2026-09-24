package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/mcp"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) mcpCmd() *cobra.Command {
	var httpAddr, token string
	cmd := &cobra.Command{
		Use:   "mcp [--http <host:port> [--token <bearer>]]",
		Short: "Serve the project's requests to AI agents over MCP (stdio, or HTTP)",
		Long: `Start a Model Context Protocol server on stdin/stdout exposing the tools
list_requests, describe_request, run_request, run_file, run_features,
list_environments, clear_session, validate_project and curl_request, plus
every .http file as a resource.

Register it with your agent, for example:
  claude mcp add api -- apic mcp --dir ./api --env dev

--http serves the streamable HTTP transport instead, for an agent on
another machine or in a container. Bind the loopback interface, or set
--token (or APIC_MCP_TOKEN): the server runs the project's requests with
the project's credentials, and refuses to listen on any other interface
without a bearer token guarding it.`,
		Example: `  apic mcp --dir ./api --env dev
  apic mcp --http 127.0.0.1:8765
  APIC_MCP_TOKEN=$(openssl rand -hex 16) apic mcp --http 0.0.0.0:8765`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := mcp.Config{Dir: a.g.dir, Env: a.g.env, Version: Version}
			if httpAddr != "" {
				if token == "" {
					token = os.Getenv("APIC_MCP_TOKEN")
				}
				err := mcp.ServeHTTP(cmd.Context(), cfg, httpAddr, token, func(bound string) {
					guard := "no token: loopback only"
					if token != "" {
						guard = "bearer token required"
					}
					fmt.Fprintf(a.Stderr, "apic mcp listening on http://%s (%s)\n", bound, guard)
				})
				if err != nil {
					return runner.Usage(runner.CodeServer, err.Error())
				}
				return nil
			}
			err := mcp.Serve(cmd.Context(), cfg)
			if errors.Is(err, io.EOF) || (err != nil && strings.HasSuffix(err.Error(), "EOF")) {
				return nil // client closed the pipe: normal shutdown
			}
			return err
		},
	}
	cmd.Flags().StringVar(&httpAddr, "http", "", "serve the streamable HTTP transport on this host:port instead of stdio")
	cmd.Flags().StringVar(&token, "token", "", "bearer token clients must send (default $APIC_MCP_TOKEN); required off the loopback interface")
	return cmd
}
