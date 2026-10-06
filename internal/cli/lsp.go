// apic lsp: the language server for editors other than VS Code.

package cli

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/lsp"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) lspCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lsp",
		Short: "Serve diagnostics, completion, hover, code lenses and formatting to an editor over LSP (stdio)",
		Long: `Start a Language Server Protocol server on stdin/stdout, for Neovim,
Helix, Zed, JetBrains IDEs or any editor with an LSP client.

It publishes apic validate's problems as you type, completes directives,
{{variables}}, selectors, operators and auth types, shows a variable's value
and source on hover (secrets masked), formats with apic fmt, and puts Run,
Describe and curl lenses above each request.

The project is found from each file: the nearest directory holding
apic.yaml or an env file, as apic itself looks. --env picks the
environment, as does initializationOptions.env from the editor.`,
		Example: `  apic lsp
  apic lsp --env staging`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The protocol owns stdout; reports written to the client's
			// log carry no colour codes.
			lipgloss.SetColorProfile(termenv.Ascii)
			runner.Version = Version
			err := lsp.Serve(cmd.Context(), a.Stdin, a.Stdout, lsp.Options{Root: a.g.dir, Env: a.g.env, Version: Version})
			if err != nil {
				return runner.Usage(runner.CodeServer, err.Error())
			}
			return nil
		},
	}
}
