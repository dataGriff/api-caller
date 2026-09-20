package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/runner"
)

func (a *App) fmtCmd() *cobra.Command {
	var check, diff bool
	cmd := &cobra.Command{
		Use:   "fmt [path...]",
		Short: "Rewrite .http files in their canonical form",
		Long: `fmt rewrites request files in place: one blank line between blocks,
directives in a fixed order (name, description, step, auth, ref, forceRef,
retry, timeout, no-redirect, no-session, no-cookies, assert, capture, then
the rest as written), header names in canonical case, query continuations
indented, JSON bodies pretty-printed when they hold no {{placeholders}},
trailing whitespace removed. Comments, unknown directives, editor script
blocks and file bodies are kept as they are, and formatting twice changes
nothing.

Without paths every .http and .rest file of the project is formatted. A
path may be a file or a directory. "-" reads stdin and writes the result
to stdout, which is what editors use.`,
		Example: `  apic fmt
  apic fmt users.http auth.http
  apic fmt --check          # exit 1 and list the files that would change (CI)
  apic fmt --diff           # show what would change
  apic fmt - < users.http`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && args[0] == "-" {
				data, err := io.ReadAll(a.Stdin)
				if err != nil {
					return &runner.UsageError{Msg: "reading stdin: " + err.Error()}
				}
				_, err = io.WriteString(a.Stdout, httpfile.Format(string(data)))
				return err
			}
			files, err := a.fmtTargets(args)
			if err != nil {
				return err
			}
			var changed []string
			for _, file := range files {
				data, err := os.ReadFile(file) //nolint:gosec // a request file of the project, or one the user named
				if err != nil {
					return &runner.UsageError{Msg: err.Error()}
				}
				formatted := httpfile.Format(string(data))
				if formatted == string(data) {
					continue
				}
				changed = append(changed, file)
				shown := a.fmtRel(file)
				switch {
				case diff:
					fmt.Fprint(a.Stdout, unifiedDiff(shown, string(data), formatted))
				case check:
					fmt.Fprintln(a.Stdout, shown)
				default:
					info, err := os.Stat(file)
					if err != nil {
						return &runner.UsageError{Msg: err.Error()}
					}
					if err := os.WriteFile(file, []byte(formatted), info.Mode().Perm()); err != nil {
						return &runner.UsageError{Msg: err.Error()}
					}
					fmt.Fprintf(a.Stdout, "formatted %s\n", shown)
				}
			}
			if a.g.json {
				rel := make([]string, 0, len(changed))
				for _, c := range changed {
					rel = append(rel, a.fmtRel(c))
				}
				if err := a.writeJSON(struct {
					Files     int      `json:"files"`
					Changed   []string `json:"changed"`
					Formatted bool     `json:"formatted"`
				}{len(files), rel, !check && !diff}); err != nil {
					return err
				}
			} else if len(changed) == 0 {
				fmt.Fprintf(a.Stdout, "%s already formatted\n", plural(len(files), "file"))
			}
			if (check || diff) && len(changed) > 0 {
				return &exitError{code: runner.ExitAssert, msg: ""}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "do not write; list the files that would change and exit 1 if any")
	cmd.Flags().BoolVar(&diff, "diff", false, "do not write; print a unified diff of what would change and exit 1 if any")
	return cmd
}

// fmtTargets resolves the files to format: the project's request files, or
// the files and directories named.
func (a *App) fmtTargets(args []string) ([]string, error) {
	if len(args) == 0 {
		p, err := a.loadProject()
		if err != nil {
			return nil, err
		}
		var files []string
		for _, f := range p.Files {
			files = append(files, filepath.Join(p.Root, f.Path))
		}
		sort.Strings(files)
		return files, nil
	}
	var files []string
	for _, arg := range args {
		path := arg
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.g.dir, arg)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, &runner.UsageError{Msg: err.Error()}
		}
		if !info.IsDir() {
			files = append(files, path)
			continue
		}
		err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if name := d.Name(); p != path && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
					return filepath.SkipDir
				}
				return nil
			}
			if ext := strings.ToLower(filepath.Ext(p)); ext == ".http" || ext == ".rest" {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, &runner.UsageError{Msg: err.Error()}
		}
	}
	return files, nil
}

// fmtRel shows a path relative to the project directory when it is under it.
func (a *App) fmtRel(file string) string {
	base, err := filepath.Abs(a.g.dir)
	if err != nil {
		return file
	}
	if rel, err := filepath.Rel(base, file); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return file
}

// unifiedDiff renders the change from a to b for one file, the way
// `diff -u` does, from a longest-common-subsequence of the lines. Files
// are small, so the quadratic table is fine.
func unifiedDiff(name, a, b string) string {
	al := strings.Split(strings.TrimSuffix(a, "\n"), "\n")
	bl := strings.Split(strings.TrimSuffix(b, "\n"), "\n")
	n, m := len(al), len(bl)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	type op struct {
		kind byte // ' ', '-', '+'
		text string
	}
	var ops []op
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && al[i] == bl[j]:
			ops = append(ops, op{' ', al[i]})
			i++
			j++
		case i < n && (j >= m || lcs[i+1][j] >= lcs[i][j+1]):
			// Deletions before insertions, the way diff -u prints them.
			ops = append(ops, op{'-', al[i]})
			i++
		default:
			ops = append(ops, op{'+', bl[j]})
			j++
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", name, name)
	const context = 3
	for k := 0; k < len(ops); {
		if ops[k].kind == ' ' {
			k++
			continue
		}
		start := k - context
		if start < 0 {
			start = 0
		}
		end := k
		for end < len(ops) {
			if ops[end].kind != ' ' {
				end++
				continue
			}
			// Stop when the next change is more than 2*context away.
			next := end
			for next < len(ops) && ops[next].kind == ' ' {
				next++
			}
			if next >= len(ops) || next-end > 2*context {
				end += context
				if end > len(ops) {
					end = len(ops)
				}
				break
			}
			end = next
		}
		aStart, bStart, aCount, bCount := 0, 0, 0, 0
		for _, o := range ops[:start] {
			if o.kind != '+' {
				aStart++
			}
			if o.kind != '-' {
				bStart++
			}
		}
		for _, o := range ops[start:end] {
			if o.kind != '+' {
				aCount++
			}
			if o.kind != '-' {
				bCount++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", aStart+1, aCount, bStart+1, bCount)
		for _, o := range ops[start:end] {
			out.WriteByte(o.kind)
			out.WriteString(o.text)
			out.WriteByte('\n')
		}
		k = end
	}
	return out.String()
}
