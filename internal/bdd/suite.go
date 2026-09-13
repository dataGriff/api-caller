package bdd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"

	"github.com/dataGriff/api-caller/internal/runner"
)

// Exit codes from Run, aligned with the rest of apic.
const (
	ExitPassed    = runner.ExitOK
	ExitFailed    = runner.ExitAssert
	ExitUsage     = runner.ExitUsage
	ExitTransport = runner.ExitTransport
)

// Formats lists the accepted --format values.
var Formats = []string{"pretty", "progress", "cucumber", "junit"}

// Options controls one run.
type Options struct {
	Config
	Paths         []string        // feature files or directories; relative paths resolve against the project root
	Features      []godog.Feature // in-memory features (tests and MCP); used when Paths is empty
	Format        string          // one of Formats; default pretty
	Output        io.Writer       // report destination; default os.Stdout
	Tags          string          // godog tag expression, e.g. "@smoke && ~@slow"
	StopOnFailure bool
	NoColors      bool
}

// Run executes the features and returns an exit code. err carries the
// typed failure when the run ended on a usage problem (exit 2: no features,
// bad phrase, unknown environment, unknown request, missing variable) or a
// transport problem (exit 3); assertion failures are reported by code only.
func Run(ctx context.Context, opts Options) (int, error) {
	if opts.Format == "" {
		opts.Format = "pretty"
	}
	valid := false
	for _, f := range Formats {
		if f == opts.Format {
			valid = true
		}
	}
	if !valid {
		return ExitUsage, fmt.Errorf("unknown format %q (one of pretty, progress, cucumber, junit)", opts.Format)
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	paths, err := opts.resolvePaths()
	if err != nil {
		return ExitUsage, err
	}
	// Fail early on unknown environments and bad phrases instead of per scenario.
	if _, err := opts.newScenario(opts.Env); err != nil {
		return ExitUsage, err
	}
	phrases, err := compilePhrases(opts.Project)
	if err != nil {
		return ExitUsage, err
	}
	output := opts.Output
	var masker *maskWriter
	if opts.Redact {
		// The cucumber report is JSON: mask its string values after the run
		// so a numeric-looking secret cannot break the structure. Text
		// formats are masked line by line as they stream.
		masker = &maskWriter{w: opts.Output, cfg: &opts.Config, whole: opts.Format == "cucumber"}
		output = masker
	}
	suite := godog.TestSuite{
		Name: "apic",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			sc.Before(opts.before)
			registerSteps(sc)
			registerPhrases(sc, phrases)
		},
		Options: &godog.Options{
			Format:          opts.Format,
			Output:          output,
			Paths:           paths,
			FeatureContents: opts.Features,
			Tags:            opts.Tags,
			Strict:          true,
			StopOnFailure:   opts.StopOnFailure,
			NoColors:        opts.NoColors,
			Concurrency:     1,
			DefaultContext:  ctx,
		},
	}
	code := suite.Run()
	if masker != nil {
		if err := masker.flush(); err != nil {
			return ExitUsage, err
		}
	}
	// Typed step errors keep the CLI's exit-code contract: definition
	// problems are 2, unreachable servers are 3, assertion failures are 1.
	if opts.usageErr != nil {
		return ExitUsage, opts.usageErr
	}
	if opts.transportErr != nil {
		return ExitTransport, opts.transportErr
	}
	switch code {
	case 0:
		return ExitPassed, nil
	case 1:
		return ExitFailed, nil
	default:
		return ExitUsage, fmt.Errorf("could not run features (check the paths and the report above)")
	}
}

// maskWriter hides registered secret values in whatever the formatter
// writes (step text, tables, doc strings, error messages). Formatters
// write in small pieces, so it buffers and masks whole lines.
type maskWriter struct {
	w     io.Writer
	cfg   *Config
	buf   bytes.Buffer
	whole bool // buffer everything and mask as JSON at flush
}

func (m *maskWriter) Write(p []byte) (int, error) {
	m.buf.Write(p)
	if m.whole {
		return len(p), nil
	}
	for {
		i := bytes.IndexByte(m.buf.Bytes(), '\n')
		if i < 0 {
			return len(p), nil
		}
		line := string(m.buf.Next(i + 1))
		if _, err := io.WriteString(m.w, m.cfg.mask(line)); err != nil {
			return 0, err
		}
	}
}

// flush writes any trailing partial line, or the whole masked JSON report.
func (m *maskWriter) flush() error {
	if m.buf.Len() == 0 {
		return nil
	}
	defer m.buf.Reset()
	if m.whole {
		if out, err := m.cfg.maskJSON(m.buf.Bytes()); err == nil {
			_, werr := m.w.Write(out)
			return werr
		}
	}
	_, err := io.WriteString(m.w, m.cfg.mask(m.buf.String()))
	return err
}

// RunSummary runs with the cucumber formatter into memory and returns the
// parsed summary plus the raw report. Used by MCP and tests. When a usage
// or transport error ends the run, the summary of what ran is still
// returned alongside the error whenever a report was produced.
func RunSummary(ctx context.Context, opts Options) (*Summary, []byte, int, error) {
	var buf bytes.Buffer
	opts.Format = "cucumber"
	opts.Output = &buf
	opts.NoColors = true
	code, err := Run(ctx, opts)
	if buf.Len() == 0 {
		return nil, nil, code, err
	}
	sum, perr := Summarize(buf.Bytes())
	if perr != nil {
		if err != nil {
			return nil, buf.Bytes(), code, err
		}
		return nil, buf.Bytes(), code, perr
	}
	// A typed step error (exit 2 or 3) still comes with the report of what ran.
	return sum, buf.Bytes(), code, err
}

func (o *Options) resolvePaths() ([]string, error) {
	if len(o.Features) > 0 && len(o.Paths) == 0 {
		return nil, nil
	}
	paths := o.Paths
	if len(paths) == 0 {
		paths = o.Project.Config.Test.Paths
	}
	if len(paths) == 0 {
		paths = []string{"features"}
	}
	root, err := filepath.EvalSymlinks(o.Project.Root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(o.Project.Root, p)
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("no features at %s (paths are relative to the project root %s)", p, o.Project.Root)
		}
		if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return nil, fmt.Errorf("feature path %s is outside the project root %s", p, o.Project.Root)
		}
		info, err := os.Stat(real)
		if err != nil {
			return nil, fmt.Errorf("no features at %s", p)
		}
		if info.IsDir() {
			n, err := checkFeatureFiles(root, real)
			if err != nil {
				return nil, err
			}
			if n == 0 {
				return nil, fmt.Errorf("no .feature files under %s", real)
			}
		}
		out = append(out, real)
	}
	return out, nil
}

// checkFeatureFiles counts the .feature files under dir and rejects any
// that resolve (through symlinks) outside the project root.
func checkFeatureFiles(root, dir string) (int, error) {
	count := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", path, err)
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".feature") {
			return nil
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("cannot resolve %s: %w", path, err)
		}
		if rel, err := filepath.Rel(root, real); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("feature file %s resolves outside the project root %s", path, root)
		}
		count++
		return nil
	})
	return count, err
}
