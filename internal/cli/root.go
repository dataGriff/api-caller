// Package cli wires the apic commands together.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
)

// Version is set at build time via -ldflags.
var Version = "dev"

type globals struct {
	dir      string
	env      string
	vars     []string
	json     bool
	noColor  bool
	timeout  time.Duration
	noSess   bool
	insecure bool
	redact   bool
}

// App holds the command tree and IO streams (swappable for tests).
type App struct {
	Root   *cobra.Command
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
	g      globals
}

// New builds the command tree.
func New() *App {
	a := &App{Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}
	root := &cobra.Command{
		Use:   "apic",
		Short: "Run .http request files from the terminal, CI, or an AI agent",
		Long: `apic runs requests defined in plain .http files — the same files VS Code,
JetBrains and Neovim can send with one click — from any terminal, CI job or
AI agent. It adds environments, captured variables that persist between
runs, assertions, JSON output, curl export and an MCP server.

Exit codes: 0 ok · 1 assertion or capture failed · 2 usage/parse/missing variable · 3 network error`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if a.g.noColor || os.Getenv("NO_COLOR") != "" || a.g.json {
				lipgloss.SetColorProfile(termenv.Ascii)
			}
		},
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&a.g.dir, "dir", "C", ".", "project root holding .http files and env files")
	pf.StringVarP(&a.g.env, "env", "e", "", "environment from http-client.env.json (default from apic.yaml)")
	pf.StringArrayVar(&a.g.vars, "var", nil, "override a variable, name=value (repeatable)")
	pf.BoolVar(&a.g.json, "json", false, "machine-readable JSON output")
	pf.BoolVar(&a.g.noColor, "no-color", false, "disable colour (also honours NO_COLOR)")
	pf.DurationVar(&a.g.timeout, "timeout", 0, "request timeout (default 30s or apic.yaml)")
	pf.BoolVar(&a.g.noSess, "no-session", false, "do not read or write captured values in .apic/session.json")
	pf.BoolVar(&a.g.insecure, "insecure", false, "skip TLS certificate verification")
	pf.BoolVar(&a.g.redact, "redact", false, "mask all request headers, bodies, query values and captures in output (for CI logs)")
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)

	root.AddCommand(a.runCmd(), a.testCmd(), a.listCmd(), a.describeCmd(), a.envCmd(), a.sessionCmd(), a.curlCmd(),
		a.validateCmd(), a.importCmd(), a.mcpCmd(), a.versionCmd())
	a.Root = root
	return a
}

// Main runs the CLI and returns the process exit code.
func Main(args []string) int {
	a := New()
	return a.Execute(context.Background(), args)
}

// Execute runs the command tree with args (excluding the program name).
func (a *App) Execute(ctx context.Context, args []string) int {
	a.Root.SetArgs(args)
	a.Root.SetOut(a.Stdout)
	a.Root.SetErr(a.Stderr)
	err := a.Root.ExecuteContext(ctx)
	if err == nil {
		return runner.ExitOK
	}
	var ec *exitError
	if errors.As(err, &ec) {
		if ec.msg != "" {
			fmt.Fprintln(a.Stderr, "error:", ec.msg)
		}
		return ec.code
	}
	code := runner.ExitCode(err)
	fmt.Fprintln(a.Stderr, "error:", err)
	return code
}

// exitError carries an explicit exit code (e.g. failed assertions).
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func (a *App) loadProject() (*project.Project, error) {
	p, err := project.Load(a.g.dir)
	if err != nil {
		return nil, &runner.UsageError{Msg: err.Error()}
	}
	return p, nil
}

func (a *App) newRunner() (*runner.Runner, error) {
	p, err := a.loadProject()
	if err != nil {
		return nil, err
	}
	vars, err := a.varMap()
	if err != nil {
		return nil, err
	}
	runner.Version = Version
	return runner.New(p, runner.Options{Env: a.g.env, Vars: vars, NoSession: a.g.noSess, Timeout: a.g.timeout, Insecure: a.g.insecure, Redact: a.g.redact})
}

func (a *App) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the apic version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(a.Stdout, "apic", Version)
		},
	}
}

// varMap parses the repeatable --var flag.
func (a *App) varMap() (map[string]string, error) {
	vars := map[string]string{}
	for _, kv := range a.g.vars {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, &runner.UsageError{Msg: fmt.Sprintf("--var %q must be name=value", kv)}
		}
		vars[k] = v
	}
	return vars, nil
}
