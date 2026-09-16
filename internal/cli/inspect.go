package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/curlexport"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/runner"
)

// theme is the shared style set; colour is switched off in root's
// PersistentPreRun when the output is not a terminal.
var theme = output.Default()

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

func (e listEntry) matches(pattern string) bool {
	p := strings.ToLower(pattern)
	return strings.Contains(strings.ToLower(e.ID), p) ||
		strings.Contains(strings.ToLower(e.URL), p) ||
		strings.Contains(strings.ToLower(e.Description), p) ||
		strings.Contains(strings.ToLower(e.File), p)
}

func (a *App) listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [pattern]",
		Short: "List every request in the project",
		Long: `List the requests apic found, grouped by file. A pattern narrows the list
to requests whose id, URL, file or description contains it.`,
		Example: `  apic list
  apic list todo
  apic list --json | jq '.requests[].id'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
				if len(args) == 1 && !e.matches(args[0]) {
					continue
				}
				entries = append(entries, e)
			}
			if a.g.json {
				if entries == nil {
					entries = []listEntry{}
				}
				return a.writeJSON(struct {
					Root     string      `json:"root"`
					Requests []listEntry `json:"requests"`
				}{p.Root, entries})
			}
			if len(entries) == 0 {
				if len(args) == 1 {
					fmt.Fprintf(a.Stdout, "no requests match %q (run `apic list` to see them all)\n", args[0])
					return nil
				}
				fmt.Fprintf(a.Stdout, "no .http files found under %s\n", p.Root)
				return nil
			}
			files := map[string]bool{}
			for _, e := range entries {
				files[e.File] = true
			}
			hasSteps := false
			for _, e := range entries {
				if len(e.Steps) > 0 {
					hasSteps = true
				}
			}
			tw := tabwriter.NewWriter(a.Stdout, 0, 4, 2, ' ', 0)
			cols := []string{"ID", "METHOD", "URL", "LINE", "DESCRIPTION"}
			if len(files) == 1 {
				cols[3] = "FILE"
			}
			if hasSteps {
				cols = append(cols, "PHRASES")
			}
			for i, c := range cols {
				cols[i] = theme.Bold.Render(c)
			}
			fmt.Fprintln(tw, strings.Join(cols, "\t"))
			lastFile := ""
			for _, e := range entries {
				if len(files) > 1 && e.File != lastFile {
					fmt.Fprintf(tw, "%s\t\t\t\t\n", theme.Accent.Render(e.File))
					lastFile = e.File
				}
				where := fmt.Sprintf("%s:%d", e.File, e.Line)
				if len(files) > 1 {
					where = fmt.Sprintf(":%d", e.Line)
				}
				line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s", theme.Bold.Render(e.ID), theme.Method(e.Method), theme.URL.Render(e.URL), theme.Dim.Render(where), e.Description)
				if hasSteps {
					line += "\t" + theme.Dim.Render(strings.Join(e.Steps, " | "))
				}
				fmt.Fprintln(tw, line)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(a.Stdout, "\n%s\n", theme.Dim.Render(fmt.Sprintf("%s in %s · apic describe <id> · apic run <id> · apic ui",
				plural(len(entries), "request"), plural(len(files), "file"))))
			return nil
		},
	}
	return cmd
}

func (a *App) describeCmd() *cobra.Command {
	cmd := &cobra.Command{
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
			fmt.Fprint(a.Stdout, output.Describe(theme, d, req.Headers))
			return nil
		},
	}
	cmd.ValidArgsFunction = a.completeRequests
	return cmd
}

func (a *App) curlCmd() *cobra.Command {
	cmd := &cobra.Command{
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
			command := curlexport.Command(res, a.g.redact)
			if a.g.json {
				return a.writeJSON(struct {
					ID      string `json:"id"`
					Command string `json:"command"`
				}{req.ID(), command})
			}
			fmt.Fprintln(a.Stdout, command)
			return nil
		},
	}
	cmd.ValidArgsFunction = a.completeRequests
	return cmd
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
						n = theme.Accent.Render(n + "*")
					}
					marked = append(marked, n)
				}
				fmt.Fprintf(a.Stdout, "%s %s\n", theme.Bold.Render("environments:"), strings.Join(marked, " "))
			}
			if len(r.Envs.Found) > 0 {
				fmt.Fprintf(a.Stdout, "%s %s\n", theme.Dim.Render("files:"), strings.Join(r.Envs.Found, ", "))
			}
			if len(vars) > 0 {
				fmt.Fprint(a.Stdout, output.Section(theme, "variables"))
				fmt.Fprint(a.Stdout, output.Variables(theme, vars, false))
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
					sev := theme.Warn.Render(d.Severity)
					if d.Severity == "error" {
						sev = theme.Fail.Render(d.Severity)
					}
					fmt.Fprintf(a.Stdout, "%s:%d: %s: %s\n", d.Path, d.Line, sev, d.Message)
				}
				counts := fmt.Sprintf("%s, %s", plural(len(p.Files), "file"), plural(len(p.Requests()), "request"))
				switch {
				case len(diags) == 0:
					fmt.Fprintf(a.Stdout, "%s %s, no problems\n", theme.OK.Render("✓"), counts)
				case errs == 0:
					fmt.Fprintf(a.Stdout, "%s %s, %s\n", theme.Warn.Render("!"), counts, plural(len(diags), "warning"))
				default:
					fmt.Fprintf(a.Stdout, "%s %s, %s, %s\n", theme.Fail.Render("✗"), counts, plural(errs, "error"), plural(len(diags)-errs, "warning"))
				}
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

// plural renders "1 file" or "3 files".
func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
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
