// apic env: the environments and the variables in effect.

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) envCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "Show environments and the variables in effect",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := a.newRunner()
			if err != nil {
				return err
			}
			vars := r.EnvVars()
			auths := r.AuthConfigs()
			if a.g.json {
				for i := range vars {
					if vars[i].Secret {
						vars[i].Value = "***"
					}
				}
				return a.writeJSON(struct {
					Root         string                  `json:"root"`
					Environments []string                `json:"environments"`
					Current      string                  `json:"current,omitempty"`
					Files        []string                `json:"files"`
					Proxy        *runner.ProxyInfo       `json:"proxy,omitempty"`
					Variables    []runner.VarInfo        `json:"variables"`
					Auth         []runner.AuthConfigInfo `json:"auth,omitempty"`
				}{r.Project.Root, r.Envs.Names(), r.Opts.Env, r.Envs.Found, r.ProxyInfo(""), vars, auths})
			}
			names := r.Envs.Names()
			if len(names) == 0 {
				fmt.Fprintf(a.Stdout, "no environments (create http-client.env.json in %s)\n", r.Project.Root)
			} else {
				var marked []string
				for _, n := range names {
					if n == r.Opts.Env {
						n = theme.Accent.Render(n + "*")
					}
					marked = append(marked, n)
				}
				fmt.Fprintf(a.Stdout, "%s %s\n", theme.Bold.Render("environments:"), strings.Join(marked, " "))
			}
			if len(r.Envs.Found) > 0 {
				fmt.Fprintf(a.Stdout, "%s %s\n", theme.Dim.Render("files:"), strings.Join(r.Envs.Found, ", "))
			}
			if p := r.ProxyInfo(""); p != nil {
				fmt.Fprintf(a.Stdout, "%s %s\n", theme.Dim.Render("proxy:"), p)
			}
			if len(vars) > 0 {
				fmt.Fprint(a.Stdout, output.Section(theme, "variables"))
				fmt.Fprint(a.Stdout, output.Variables(theme, vars, false))
			}
			if len(auths) > 0 {
				// JetBrains Security.Auth: what {{$auth.token("name")}} runs.
				fmt.Fprint(a.Stdout, output.Section(theme, "auth"))
				for _, c := range auths {
					what := c.Spec
					if c.Error != "" {
						what = theme.Fail.Render(c.Error)
					}
					fmt.Fprintf(a.Stdout, "  %s %s %s\n", theme.Bold.Render(c.Name), what, theme.Dim.Render("("+strings.Join(c.Files, ", ")+")"))
				}
			}
			return nil
		},
	}
}
