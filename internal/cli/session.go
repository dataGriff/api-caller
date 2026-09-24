// apic session: captured values and cookies, and clearing them.

package cli

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/runner"
	"github.com/dataGriff/api-caller/internal/session"
)

func (a *App) sessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Show captured values stored for later runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := a.newRunner()
			if err != nil {
				return err
			}
			if r.Session == nil {
				return runner.Usage(runner.CodeSession, "session disabled by --no-session")
			}
			if a.g.json {
				return a.writeJSON(maskSessionEnvs(r.Session.Envs, time.Now()))
			}
			envs := r.Session.EnvNames()
			jar := r.Jar
			if jar == nil {
				// Cookies are listed whether or not this command switched
				// the jar on: they are in the session directory either way.
				if jar, err = session.OpenJar(r.Project.Root); err != nil {
					return runner.Usage(runner.CodeSession, "cookies: "+err.Error())
				}
			}
			for _, e := range jar.EnvNames() {
				if !slices.Contains(envs, e) {
					envs = append(envs, e)
				}
			}
			sort.Strings(envs)
			if len(envs) == 0 {
				fmt.Fprintln(a.Stdout, "session is empty")
				return nil
			}
			for _, e := range envs {
				fmt.Fprintln(a.Stdout, theme.Bold.Render(e))
				vars := r.Session.Vars(e)
				keys := make([]string, 0, len(vars))
				for k := range vars {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					if isAuthCacheKey(k) {
						fmt.Fprintf(a.Stdout, "  %s %s = %s\n", theme.Capture.Render("↳"), k, auth.DescribeCached(vars[k], time.Now()))
						continue
					}
					fmt.Fprintf(a.Stdout, "  %s %s = %s\n", theme.Capture.Render("↳"), k, output.Truncate(vars[k], 60))
				}
				for _, c := range jar.Cookies(e) {
					fmt.Fprintf(a.Stdout, "  %s cookie %s = %s %s\n", theme.Capture.Render("↳"), c.Name, runner.Masked, theme.Dim.Render(fmt.Sprintf("(%s%s · %s)", c.Domain, c.Path, c.ExpiryText(time.Now()))))
				}
			}
			return nil
		},
	}
	cookies := &cobra.Command{
		Use:   "cookies",
		Short: "List the cookies stored per environment (values masked)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := a.loadProject()
			if err != nil {
				return err
			}
			jar, err := session.OpenJar(p.Root)
			if err != nil {
				return runner.Usage(runner.CodeSession, "cookies: "+err.Error())
			}
			out := map[string][]cookieInfo{}
			for _, e := range jar.EnvNames() {
				for _, c := range jar.Cookies(e) {
					out[e] = append(out[e], cookieInfo{Name: c.Name, Domain: c.Domain, Path: c.Path, Expires: c.Expires, Secure: c.Secure, HTTPOnly: c.HTTPOnly})
				}
			}
			if a.g.json {
				return a.writeJSON(out)
			}
			if len(out) == 0 {
				fmt.Fprintln(a.Stdout, "no cookies stored")
				return nil
			}
			for _, e := range jar.EnvNames() {
				fmt.Fprintln(a.Stdout, theme.Bold.Render(e))
				for _, c := range jar.Cookies(e) {
					fmt.Fprintf(a.Stdout, "  %s %s = %s %s\n", theme.Capture.Render("↳"), c.Name, runner.Masked, theme.Dim.Render(fmt.Sprintf("(%s%s · %s)", c.Domain, c.Path, c.ExpiryText(time.Now()))))
				}
			}
			return nil
		},
	}
	cmd.AddCommand(cookies)
	var all bool
	clear := &cobra.Command{
		Use:   "clear",
		Short: "Forget captured values for the current environment (or --all)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := a.newRunner()
			if err != nil {
				return err
			}
			if r.Session == nil {
				return runner.Usage(runner.CodeSession, "session disabled by --no-session")
			}
			target := r.Opts.Env
			if all {
				target = "*"
			}
			r.Session.Clear(target)
			if err := r.Session.Save(); err != nil {
				return err
			}
			// Cookies go with the captures, switched on or not.
			jar, err := session.OpenJar(r.Project.Root)
			if err != nil {
				return runner.Usage(runner.CodeSession, "cookies: "+err.Error())
			}
			jar.Clear(target)
			if err := jar.Save(); err != nil {
				return err
			}
			if a.g.json {
				return a.writeJSON(map[string]string{"cleared": target})
			}
			fmt.Fprintln(a.Stdout, "session cleared")
			return nil
		},
	}
	clear.Flags().BoolVar(&all, "all", false, "clear every environment")
	cmd.AddCommand(clear)
	return cmd
}

// cookieInfo is what `apic session cookies --json` prints per cookie: the
// value stays out, like a secret.
type cookieInfo struct {
	Name     string    `json:"name"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitzero"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"http_only,omitempty"`
}

func maskSessionEnvs(envs map[string]map[string]string, now time.Time) map[string]map[string]string {
	out := make(map[string]map[string]string, len(envs))
	for env, vars := range envs {
		masked := make(map[string]string, len(vars))
		for k, v := range vars {
			if isAuthCacheKey(k) {
				masked[k] = auth.DescribeCached(v, now)
				continue
			}
			masked[k] = v
		}
		out[env] = masked
	}
	return out
}

func isAuthCacheKey(k string) bool {
	return strings.HasPrefix(k, "$oauth2:") || strings.HasPrefix(k, "$exec:")
}
