package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/bdd"
	"github.com/dataGriff/api-caller/internal/env"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
	"github.com/dataGriff/api-caller/internal/session"
)

func (a *App) testCmd() *cobra.Command {
	var format, output, tags string
	var stopOnFailure, useSession, listSteps bool
	cmd := &cobra.Command{
		Use:   "test [path|file.feature]...",
		Short: "Run Gherkin feature files against the project's requests",
		Long: `Run .feature files with a built-in step vocabulary, no Cucumber runtime
needed. Requests are referenced by id ("When I run \"get-user\"") or through
phrases declared on them with "# @step" in the .http file.

Each scenario starts with a fresh, in-memory session so tests never touch
.apic/session.json (use --use-session to change that). Undefined steps fail
the run.

Exit codes: 0 all scenarios passed · 1 assertion failures or undefined
steps · 2 definition problems (no features, feature path outside the
project, unknown environment, unknown request, missing variable, bad
phrase) · 3 a server could not be reached.`,
		Example: `  apic test
  apic test features/users.feature --env staging --tags @smoke
  apic test --format junit --output report.xml
  apic test --json | jq '.[].elements[].steps[].result.status'
  apic test --steps`,
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			if listSteps {
				return a.printSteps()
			}
			p, err := a.loadProject()
			if err != nil {
				return err
			}
			vars, err := a.varMap()
			if err != nil {
				return err
			}
			runner.Version = Version
			if useSession && a.g.noSess {
				return &runner.UsageError{Msg: "--use-session and --no-session cannot be combined"}
			}
			opts := bdd.Options{
				Config: bdd.Config{Project: p, Env: a.g.env, Vars: vars, UseSession: useSession,
					Timeout: a.g.timeout, Insecure: a.g.insecure, Redact: a.g.redact, Stderr: a.Stderr},
				Paths: args, Format: format, Tags: tags, StopOnFailure: stopOnFailure,
				NoColors: a.g.noColor || a.g.json || os.Getenv("NO_COLOR") != "" || output != "" || !isTerminal(a.Stdout),
				Output:   a.Stdout,
			}
			if opts.Env == "" {
				opts.Env = p.Config.Env
			}
			if a.g.json {
				opts.Format = "cucumber"
			}
			if output != "" {
				features, err := bdd.FeatureFiles(opts)
				if err != nil {
					return &runner.UsageError{Msg: err.Error()}
				}
				if err := outputOverlapsSources(output, p, features); err != nil {
					return err
				}
				// Created on first write, which happens only after the features
				// have been read, so a report path can never truncate its input.
				lf := &lazyFile{path: output}
				defer func() {
					// A report that was not fully written fails the command
					// whatever the scenarios did: CI must not lose it silently.
					if cerr := lf.Close(); cerr != nil {
						retErr = &runner.UsageError{Msg: fmt.Sprintf("write report %s: %v (run result: %v)", output, cerr, describeOutcome(retErr))}
					}
				}()
				opts.Output = lf
			}
			code, err := bdd.Run(cmd.Context(), opts)
			if err != nil {
				var te *runner.TransportError
				var ue *runner.UsageError
				if errors.As(err, &te) || errors.As(err, &ue) {
					return err // keeps exit codes 3 and 2
				}
				return &runner.UsageError{Msg: err.Error()}
			}
			if code != 0 {
				return &exitError{code: code}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&format, "format", "f", "pretty", "report format: "+strings.Join(bdd.Formats, ", "))
	cmd.Flags().StringVarP(&output, "output", "o", "", "write the report to a file instead of stdout")
	cmd.Flags().StringVarP(&tags, "tags", "t", "", `tag expression, e.g. "@smoke && ~@slow"`)
	cmd.Flags().BoolVar(&stopOnFailure, "stop-on-failure", false, "stop after the first failed scenario")
	cmd.Flags().BoolVar(&useSession, "use-session", false, "read and write .apic/session.json instead of an isolated session per scenario")
	cmd.Flags().BoolVar(&listSteps, "steps", false, "print the built-in step vocabulary and declared phrases, then exit")
	return cmd
}

// isTerminal reports whether w is an interactive terminal; reports written
// to files or pipes get no colour.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// realPath returns path in absolute, symlink-free, canonical form even when
// it (or some of its parent directories) does not exist yet: the nearest
// existing ancestor is resolved and the remainder appended. On Windows this
// also turns 8.3 short names such as RUNNER~1 into their long form.
func realPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rest := ""
	for cur := abs; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// describeOutcome words the run result for the report-failure message.
func describeOutcome(err error) string {
	if err == nil {
		return "scenarios passed"
	}
	return err.Error()
}

// outputOverlapsSources refuses a report path that is, or aliases through a
// symlink or hard link, a file the test command reads or writes: request,
// feature and body files, the project config, environment files and the
// session. features are the resolved feature files selected for this run.
func outputOverlapsSources(output string, p *project.Project, features []string) error {
	refuse := func() error {
		return &runner.UsageError{Msg: fmt.Sprintf("--output %s would overwrite a project file; write the report elsewhere", output)}
	}
	bodyFiles := map[string]bool{}
	for _, req := range p.Requests() {
		if req.BodyFile == "" {
			continue
		}
		path := filepath.Join(p.Root, filepath.Dir(req.File.Path), req.BodyFile)
		if abs, err := filepath.Abs(path); err == nil {
			bodyFiles[filepath.Clean(abs)] = true
		}
	}
	rootReal := realPath(p.Root)
	// underRoot reports whether path (existing or not) lies in the project.
	underRoot := func(path string) bool {
		rel, err := filepath.Rel(rootReal, realPath(path))
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
	}
	// isInput recognises project inputs by name only inside the project;
	// files elsewhere are matched by identity below.
	isInput := func(path string) bool {
		if abs, err := filepath.Abs(path); err == nil && bodyFiles[filepath.Clean(abs)] {
			return true
		}
		if !underRoot(path) {
			return false
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".feature", ".http", ".rest":
			return true
		}
		switch filepath.Base(path) {
		case project.ConfigFile, env.PublicFile, env.PrivateFile, env.DotEnvFile:
			return true
		}
		return filepath.Base(filepath.Dir(path)) == session.Dir
	}
	if isInput(output) {
		return refuse()
	}
	real, err := filepath.EvalSymlinks(output)
	if err != nil {
		return nil // does not exist yet: nothing to alias
	}
	if isInput(real) {
		return refuse()
	}
	target, err := os.Stat(real)
	if err != nil {
		return nil
	}
	sameAs := func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && os.SameFile(info, target)
	}
	// Explicitly selected inputs are checked directly, wherever they live.
	for bf := range bodyFiles {
		if sameAs(bf) {
			return refuse()
		}
	}
	for _, f := range features {
		if sameAs(f) {
			return refuse()
		}
	}
	// Request files can live outside the root (`dir: ../api` in apic.yaml).
	for _, f := range p.Files {
		if sameAs(filepath.Join(p.Root, filepath.FromSlash(f.Path))) {
			return refuse()
		}
	}
	// Fixed project files, for an output that aliases them from elsewhere.
	for _, name := range []string{project.ConfigFile, env.PublicFile, env.PrivateFile, env.DotEnvFile, filepath.Join(session.Dir, session.File)} {
		if sameAs(filepath.Join(p.Root, name)) {
			return refuse()
		}
	}
	var found bool
	_ = filepath.WalkDir(p.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			if path != p.Root && d.Name() != session.Dir && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if isInput(path) && sameAs(path) {
			found = true
		}
		return nil
	})
	if found {
		return refuse()
	}
	return nil
}

// lazyFile opens its path on the first write. The first create or write
// error is kept and reported by Close, since formatters may ignore it.
type lazyFile struct {
	path string
	f    *os.File
	err  error
}

func (l *lazyFile) Write(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	if l.f == nil {
		f, err := os.Create(l.path)
		if err != nil {
			l.err = err
			return 0, err
		}
		l.f = f
	}
	n, err := l.f.Write(p)
	if err != nil {
		l.err = err
	}
	return n, err
}

func (l *lazyFile) Close() error {
	if l.f == nil {
		return l.err
	}
	if err := l.f.Close(); err != nil && l.err == nil {
		l.err = err
	}
	return l.err
}

func (a *App) printSteps() error {
	if a.g.json {
		type entry struct {
			Pattern string `json:"pattern"`
			Purpose string `json:"purpose"`
			Request string `json:"request,omitempty"`
		}
		var out []entry
		for _, v := range bdd.Vocabulary {
			out = append(out, entry{Pattern: v.Pattern, Purpose: v.Purpose})
		}
		// Project phrases are appended when a project loads; the built-in
		// vocabulary is still useful outside one.
		if p, err := a.loadProject(); err == nil {
			for _, r := range p.Requests() {
				for _, st := range r.Steps() {
					out = append(out, entry{Pattern: st, Purpose: "runs " + r.ID(), Request: r.ID()})
				}
			}
		}
		return a.writeJSON(out)
	}
	fmt.Fprintln(a.Stdout, theme.Bold.Render("built-in steps"))
	for _, v := range bdd.Vocabulary {
		fmt.Fprintf(a.Stdout, "  %s\n      %s\n", v.Pattern, theme.Dim.Render(v.Purpose))
	}
	p, err := a.loadProject()
	if err != nil {
		return nil // vocabulary alone is still useful outside a project
	}
	printed := false
	for _, r := range p.Requests() {
		for _, st := range r.Steps() {
			if !printed {
				fmt.Fprintln(a.Stdout, theme.Bold.Render("\nphrases declared in this project"))
				printed = true
			}
			fmt.Fprintf(a.Stdout, "  %s\n      %s\n", st, theme.Dim.Render("runs "+r.ID()+" ("+r.File.Path+")"))
		}
	}
	return nil
}
