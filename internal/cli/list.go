// apic list: every request in the project, filtered by a pattern.

package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/httpfile"
)

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
	Refs        []string `json:"refs,omitempty"`
	Disabled    bool     `json:"disabled,omitempty"`
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
				e := listEntry{ID: r.ID(), Name: r.Name, Method: r.Method, URL: r.URL, File: r.File.Path, Line: r.Line, Description: r.Description, Asserts: len(r.Asserts), Steps: r.Steps(), Refs: refIDs(r), Disabled: r.Disabled()}
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
				id, desc := theme.Bold.Render(e.ID), e.Description
				if e.Disabled {
					id, desc = theme.Dim.Render(e.ID), strings.TrimSpace(theme.Dim.Render("(disabled)")+" "+desc)
				}
				line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s", id, theme.Method(e.Method), theme.URL.Render(e.URL), theme.Dim.Render(where), desc)
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

// refIDs lists the targets of a request's `# @ref` and `# @forceRef` directives.
func refIDs(r *httpfile.Request) []string {
	var out []string
	for _, ref := range r.Refs() {
		out = append(out, ref.ID)
	}
	return out
}
