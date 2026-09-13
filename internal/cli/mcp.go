package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/mcp"
)

func (a *App) mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the project's requests to AI agents over MCP (stdio)",
		Long: `Start a Model Context Protocol server on stdin/stdout exposing the tools
list_requests, describe_request, run_request, run_file, run_features,
list_environments and clear_session, plus every .http file as a resource.

Register it with your agent, for example:
  claude mcp add api -- apic mcp --dir ./api --env dev`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := mcp.Serve(cmd.Context(), mcp.Config{Dir: a.g.dir, Env: a.g.env, Version: Version})
			if errors.Is(err, io.EOF) || (err != nil && strings.HasSuffix(err.Error(), "EOF")) {
				return nil // client closed the pipe: normal shutdown
			}
			return err
		},
	}
}
