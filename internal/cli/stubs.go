package cli

import "github.com/spf13/cobra"

func (a *App) importCmd() *cobra.Command {
	return &cobra.Command{Use: "import", Short: "Scaffold .http files from an OpenAPI document", Hidden: true}
}
