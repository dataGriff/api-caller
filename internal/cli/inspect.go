package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/curlexport"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/runner"
)

var (
	styleBold = lipgloss.NewStyle().Bold(true)
	styleDim  = lipgloss.NewStyle().Faint(true)
	styleBad  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styleGood = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

func (a *App) writeJSON(v any) error {
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

type listEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Method      string   `json:"method"`
	URL         string   `json:"url"`
	File        string   `json:"file"`
	Line        int      `json:"line"`
	Description string   `json:"description,omitempty"`
	Captures    []string `json:"captures,omitempty"`
	Asserts     int      `json:"asserts,omitempty"`
	Steps       []string `json:"steps,omitempty"`
}

func (a *App) listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every request in the project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := a.loadProject()
			if err != nil {
				return err
			}
			var entries []listEntry
			for _, r := range p.Requests() {
				e := listEntry{ID: r.ID(), Name: r.Name, Method: r.Method, URL: r.URL, File: r.File.Path, Line: r.Line, Description: r.Description, Asserts: len(r.Asserts), Steps: r.Steps()}
				for _, c := range r.Captures {
					e.Captures = append(e.Captures, c.Name)
				}
				entries = append(entries, e)
			}
			if a.g.json {
				return a.writeJSON(struct {
					Root     string      `json:"root"`
					Requests []listEntry `json:"requests"`
				}{p.Root, entries})
			}
			if len(entries) == 0 {
				fmt.Fprintf(a.Stdout, "no .http files found under %s\n", p.Root)
				return nil
			}
			tw := tabwriter.NewWriter(a.Stdout, 0, 4, 2, ' ', 0)
			hasSteps := false
			for _, e := range entries {
				if len(e.Steps) > 0 {
					hasSteps = true
				}
			}
			header := styleBold.Render("ID") + "\t" + styleBold.Render("METHOD") + "\t" + styleBold.Render("URL") + "\t" + styleBold.Render("FILE") + "\t" + styleBold.Render("DESCRIPTION")
			if hasSteps {
				header += "\t" + styleBold.Render("PHRASES")
			}
			fmt.Fprintln(tw, header)
			for _, e := range entries {
				line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s", e.ID, e.Method, e.URL, fmt.Sprintf("%s:%d", e.File, e.Line), e.Description)
				if hasSteps {
					line += "\t" + strings.Join(e.Steps, " | ")
				}
				fmt.Fprintln(tw, line)
			}
			return tw.Flush()
		},
	}
}

func (a *App) describeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe <request>",
		Short: "Show a request's variables, where each comes from, captures and asserts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, req, err := a.single(args[0])
			if err != nil {
				return err
			}
			d := r.Describe(req)
			if a.g.json {
				return a.writeJSON(maskDescription(d))
			}
			fmt.Fprintf(a.Stdout, "%s %s\n", styleBold.Render(d.Method), d.URLTemplate)
			if d.Description != "" {
				fmt.Fprintln(a.Stdout, d.Description)
			}
			fmt.Fprintf(a.Stdout, "%s %s:%d\n", styleDim.Render("file:"), d.File, d.Line)
			if d.ID != d.Name {
				fmt.Fprintf(a.Stdout, "%s %s\n", styleDim.Render("id:  "), d.ID)
			}
			if len(d.Headers) > 0 {
				section(a.Stdout, "headers")
				for _, h := range req.Headers {
					fmt.Fprintf(a.Stdout, "  %s: %s\n", h.Name, h.Value)
				}
			}
			if d.Body != "" {
				section(a.Stdout, "body")
				fmt.Fprintln(a.Stdout, indent(d.Body))
			}
			if d.Auth != "" {
				section(a.Stdout, "auth")
				fmt.Fprintf(a.Stdout, "  %s %s\n", d.Auth, styleDim.Render("("+d.AuthSource+")"))
			}
			if d.BodyFile != "" {
				section(a.Stdout, "body file")
				fmt.Fprintln(a.Stdout, "  "+d.BodyFile)
			}
			section(a.Stdout, "variables")
			if len(d.Variables) == 0 {
				fmt.Fprintln(a.Stdout, "  (none)")
			}
			tw := tabwriter.NewWriter(a.Stdout, 0, 4, 2, ' ', 0)
			for _, v := range d.Variables {
				switch {
				case v.Missing && v.CapturedBy != "":
					fmt.Fprintf(tw, "  %s\t%s\t%s\n", styleBad.Render("✗ "+v.Name), "missing", fmt.Sprintf("captured by %s — run `apic run %s` first", v.CapturedBy, v.CapturedBy))
				case v.Missing:
					fmt.Fprintf(tw, "  %s\t%s\t%s\n", styleBad.Render("✗ "+v.Name), "missing", "pass --var "+v.Name+"=...")
				default:
					fmt.Fprintf(tw, "  %s\t%s\t%s\n", styleGood.Render("✓ "+v.Name), mask(v), styleDim.Render(v.Source))
				}
			}
			tw.Flush()
			if steps := req.Steps(); len(steps) > 0 {
				section(a.Stdout, "steps")
				for _, st := range steps {
					fmt.Fprintf(a.Stdout, "  %s\n", st)
				}
			}
			if len(d.Captures) > 0 {
				section(a.Stdout, "captures")
				for _, c := range d.Captures {
					fmt.Fprintf(a.Stdout, "  %s\n", c)
				}
			}
			if len(d.Asserts) > 0 {
				section(a.Stdout, "asserts")
				for _, x := range d.Asserts {
					fmt.Fprintf(a.Stdout, "  %s\n", x)
				}
			}
			if d.Ready {
				fmt.Fprintf(a.Stdout, "\n%s %s\n", styleGood.Render("ready:"), d.URL)
			} else {
				fmt.Fprintf(a.Stdout, "\n%s\n", styleWarn.Render("not ready: some variables are missing"))
			}
			return nil
		},
	}
}

func (a *App) curlCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "curl <request>",
		Short: "Print the equivalent curl command (with variables resolved)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, req, err := a.single(args[0])
			if err != nil {
				return err
			}
			res, err := r.Resolve(req)
			if err != nil {
				return err
			}
			if d := r.Describe(req); !d.Ready {
				var missing []string
				for _, v := range d.Variables {
					if v.Missing {
						missing = append(missing, v.Name)
					}
				}
				return r.MissingError(req, missing)
			}
			fmt.Fprintln(a.Stdout, curlexport.Command(res))
			return nil
		},
	}
}

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
			if a.g.json {
				for i := range vars {
					if vars[i].Secret {
						vars[i].Value = "***"
					}
				}
				return a.writeJSON(struct {
					Root         string           `json:"root"`
					Environments []string         `json:"environments"`
					Current      string           `json:"current,omitempty"`
					Files        []string         `json:"files"`
					Variables    []runner.VarInfo `json:"variables"`
				}{r.Project.Root, r.Envs.Names(), r.Opts.Env, r.Envs.Found, vars})
			}
			names := r.Envs.Names()
			if len(names) == 0 {
				fmt.Fprintf(a.Stdout, "no environments (create http-client.env.json in %s)\n", r.Project.Root)
			} else {
				var marked []string
				for _, n := range names {
					if n == r.Opts.Env {
						n = styleBold.Render(n + "*")
					}
					marked = append(marked, n)
				}
				fmt.Fprintf(a.Stdout, "%s %s\n", styleBold.Render("environments:"), strings.Join(marked, " "))
			}
			if len(r.Envs.Found) > 0 {
				fmt.Fprintf(a.Stdout, "%s %s\n", styleDim.Render("files:"), strings.Join(r.Envs.Found, ", "))
			}
			if len(vars) > 0 {
				section(a.Stdout, "variables")
				tw := tabwriter.NewWriter(a.Stdout, 0, 4, 2, ' ', 0)
				for _, v := range vars {
					fmt.Fprintf(tw, "  %s\t%s\t%s\n", v.Name, mask(v), styleDim.Render(v.Source))
				}
				tw.Flush()
			}
			return nil
		},
	}
}

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
				return &runner.UsageError{Msg: "session disabled by --no-session"}
			}
			if a.g.json {
				return a.writeJSON(maskSessionEnvs(r.Session.Envs, time.Now()))
			}
			envs := r.Session.EnvNames()
			if len(envs) == 0 {
				fmt.Fprintln(a.Stdout, "session is empty")
				return nil
			}
			for _, e := range envs {
				fmt.Fprintln(a.Stdout, styleBold.Render(e))
				vars := r.Session.Vars(e)
				keys := make([]string, 0, len(vars))
				for k := range vars {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					if isAuthCacheKey(k) {
						fmt.Fprintf(a.Stdout, "  %s = %s\n", k, auth.DescribeCached(vars[k], time.Now()))
						continue
					}
					fmt.Fprintf(a.Stdout, "  %s = %s\n", k, truncate(vars[k], 60))
				}
			}
			return nil
		},
	}
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
				return &runner.UsageError{Msg: "session disabled by --no-session"}
			}
			target := r.Opts.Env
			if all {
				target = "*"
			}
			r.Session.Clear(target)
			if err := r.Session.Save(); err != nil {
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

func (a *App) validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Parse every .http file and report problems (for CI)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := a.loadProject()
			if err != nil {
				return err
			}
			diags := p.Validate()
			errs := 0
			for _, d := range diags {
				if d.Severity == "error" {
					errs++
				}
			}
			if a.g.json {
				if diags == nil {
					diags = []httpfile.Diagnostic{}
				}
				if err := a.writeJSON(struct {
					OK          bool                  `json:"ok"`
					Files       int                   `json:"files"`
					Requests    int                   `json:"requests"`
					Diagnostics []httpfile.Diagnostic `json:"diagnostics"`
				}{errs == 0, len(p.Files), len(p.Requests()), diags}); err != nil {
					return err
				}
			} else {
				for _, d := range diags {
					sev := styleWarn.Render(d.Severity)
					if d.Severity == "error" {
						sev = styleBad.Render(d.Severity)
					}
					fmt.Fprintf(a.Stdout, "%s:%d: %s: %s\n", d.Path, d.Line, sev, d.Message)
				}
				fmt.Fprintf(a.Stdout, "%d file(s), %d request(s), %d error(s), %d warning(s)\n", len(p.Files), len(p.Requests()), errs, len(diags)-errs)
			}
			if errs > 0 {
				return &exitError{code: runner.ExitUsage}
			}
			return nil
		},
	}
}

// single loads the runner and resolves exactly one request.
func (a *App) single(target string) (*runner.Runner, *httpfile.Request, error) {
	r, err := a.newRunner()
	if err != nil {
		return nil, nil, err
	}
	reqs, err := r.Project.Resolve(target)
	if err != nil {
		return nil, nil, &runner.UsageError{Msg: err.Error()}
	}
	if len(reqs) != 1 {
		return nil, nil, &runner.UsageError{Msg: fmt.Sprintf("%s names %d requests; pick one with %s#<name>", target, len(reqs), target)}
	}
	return r, reqs[0], nil
}

func maskDescription(d *runner.Description) *runner.Description {
	out := *d
	out.Variables = append([]runner.VarInfo(nil), d.Variables...)
	for i := range out.Variables {
		if out.Variables[i].Secret {
			out.Variables[i].Value = "***"
		}
	}
	return &out
}

func mask(v runner.VarInfo) string {
	if v.Secret {
		return "***"
	}
	return truncate(v.Value, 60)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n  ")
}

func section(w io.Writer, title string) {
	fmt.Fprintf(w, "\n%s\n", styleBold.Render(title))
}
