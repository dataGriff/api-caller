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
	ExitPassed = runner.ExitOK
	ExitFailed = runner.ExitAssert
	ExitUsage  = runner.ExitUsage
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

// Run executes the features and returns an exit code. err is set only for
// usage problems (no features, bad phrase, unknown environment).
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
	var phraseErr error
	suite := godog.TestSuite{
		Name: "apic",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			sc.Before(opts.before)
			registerSteps(sc)
			if err := registerPhrases(sc, opts.Project); err != nil && phraseErr == nil {
				phraseErr = err
			}
		},
		Options: &godog.Options{
			Format:          opts.Format,
			Output:          opts.Output,
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
	if phraseErr != nil {
		return ExitUsage, phraseErr
	}
	if opts.usageErr != nil {
		return ExitUsage, opts.usageErr
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

// RunSummary runs with the cucumber formatter into memory and returns the
// parsed summary plus the raw report. Used by MCP and --json.
func RunSummary(ctx context.Context, opts Options) (*Summary, []byte, int, error) {
	var buf bytes.Buffer
	opts.Format = "cucumber"
	opts.Output = &buf
	opts.NoColors = true
	code, err := Run(ctx, opts)
	if err != nil {
		return nil, nil, code, err
	}
	sum, perr := Summarize(buf.Bytes())
	if perr != nil {
		return nil, buf.Bytes(), code, perr
	}
	return sum, buf.Bytes(), code, nil
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
	var out []string
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(o.Project.Root, p)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("no features at %s (paths are relative to the project root %s)", p, o.Project.Root)
		}
		if info.IsDir() && !containsFeature(abs) {
			return nil, fmt.Errorf("no .feature files under %s", abs)
		}
		out = append(out, abs)
	}
	return out, nil
}

// containsFeature reports whether a directory holds at least one .feature file.
func containsFeature(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".feature") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}
