// Package cli wires the apic commands together.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/demoapi"
	"github.com/dataGriff/api-caller/internal/env"
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
	cookies  bool
	proxy    string
	noProxy  bool
	cacert   string
	cert     string
	key      string
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
	demoapi.Version = Version // GET /health on the demo API reports it
	root := &cobra.Command{
		Use:   "apic",
		Short: "Run .http request files from the terminal, CI, or an AI agent",
		Long: `apic is epic: it runs the plain .http files VS Code, JetBrains and Neovim
can send with one click, from any terminal, CI job or AI agent, with one
static binary. It adds environments, captured variables that persist between
runs, assertions, AWS and OAuth2 auth, Gherkin features, JSON output, curl
export, an MCP server and a terminal UI (apic ui).

Try it with nothing set up:  apic ui --demo

Exit codes: 0 ok · 1 assertion or capture failed · 2 usage/parse/missing variable · 3 network error`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if a.g.noColor || os.Getenv("NO_COLOR") != "" || a.g.json || !isTerminal(a.Stdout) {
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
	pf.StringVar(&a.g.cacert, "cacert", "", "PEM file with certificates to trust in addition to the system roots")
	pf.StringVar(&a.g.cert, "cert", "", "PEM client certificate to present (mTLS)")
	pf.StringVar(&a.g.key, "key", "", "PEM private key for --cert (default: the --cert file)")
	pf.StringVar(&a.g.proxy, "proxy", "", "send requests through this proxy (http, https or socks5 URL); beats proxy: in apic.yaml and HTTP(S)_PROXY")
	pf.BoolVar(&a.g.noProxy, "no-proxy", false, "send requests directly, ignoring --proxy, apic.yaml and the environment")
	pf.BoolVar(&a.g.cookies, "cookies", false, "keep a cookie jar per environment in .apic/cookies.json (or set cookies: true in apic.yaml)")
	pf.BoolVar(&a.g.redact, "redact", false, "mask all request headers, bodies, query values and captures in output (for CI logs)")
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)

	root.AddCommand(a.runCmd(), a.uiCmd(), a.testCmd(), a.listCmd(), a.describeCmd(), a.envCmd(), a.sessionCmd(), a.curlCmd(), a.snippetCmd(),
		a.validateCmd(), a.fmtCmd(), a.importCmd(), a.initCmd(), a.mcpCmd(), a.demoCmd(), a.versionCmd())
	_ = root.RegisterFlagCompletionFunc("env", a.completeEnvs)
	codeArgumentErrors(root)
	a.Root = root
	return a
}

// codeArgumentErrors gives cobra's own errors, a bad flag value or the
// wrong number of arguments, the code of any other bad flag.
func codeArgumentErrors(c *cobra.Command) {
	c.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return runner.Usage(runner.CodeFlag, err.Error())
	})
	if args := c.Args; args != nil {
		c.Args = func(cmd *cobra.Command, a []string) error {
			if err := args(cmd, a); err != nil {
				return runner.Usage(runner.CodeFlag, err.Error())
			}
			return nil
		}
	}
	for _, sub := range c.Commands() {
		codeArgumentErrors(sub)
	}
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
	if runner.CodeOf(err) == runner.CodeOther && strings.HasPrefix(err.Error(), "unknown command") {
		err = runner.Usage(runner.CodeFlag, err.Error())
		// cobra stops before parsing flags for a command it cannot find,
		// so --json has to be read from the arguments.
		a.g.json = a.g.json || slices.Contains(args, "--json")
	}
	code := runner.ExitCode(err)
	a.printError(err, code)
	return code
}

// printError writes an error to stderr: under --json as an object an
// agent can branch on, otherwise as a line, followed on a terminal by the
// code and where the catalogue explains it.
func (a *App) printError(err error, exit int) {
	info := runner.Info(err)
	info.Exit = exit
	if a.g.json {
		enc := json.NewEncoder(a.Stderr)
		enc.SetEscapeHTML(false) // hints say <command>
		_ = enc.Encode(map[string]any{"error": info})
		return
	}
	fmt.Fprintln(a.Stderr, "error:", err)
	if isTerminal(a.Stderr) {
		fmt.Fprintln(a.Stderr, theme.Dim.Render(fmt.Sprintf("  %s %s · see %s", info.Code, info.Title, info.URL)))
	}
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
		return nil, runner.Usage(runner.CodeProject, err.Error())
	}
	return p, nil
}

func (a *App) newRunner() (*runner.Runner, error) {
	p, err := a.loadProject()
	if err != nil {
		return nil, err
	}
	return a.runnerFor(p, nil)
}

// runnerFor builds a runner on an already loaded project from the global
// flags; mod, when set, adjusts the options first.
func (a *App) runnerFor(p *project.Project, mod func(*runner.Options)) (*runner.Runner, error) {
	vars, err := a.varMap()
	if err != nil {
		return nil, err
	}
	runner.Version = Version
	opts := runner.Options{Env: a.g.env, Vars: vars, NoSession: a.g.noSess, Timeout: a.g.timeout, Insecure: a.g.insecure, Redact: a.g.redact, Cookies: a.g.cookies, CACert: a.g.cacert, Cert: a.g.cert, Key: a.g.key, Proxy: a.g.proxy, NoProxy: a.g.noProxy}
	if mod != nil {
		mod(&opts)
	}
	r, err := runner.New(p, opts)
	if err != nil {
		return nil, err
	}
	// A browser sign-in (oauth2 grant=authorization_code) may start only
	// with a person present: a terminal on both sides and no --json.
	r.Interactive = !a.g.json && isTerminal(a.Stderr) && isTerminal(os.Stdin)
	return r, nil
}

func (a *App) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the apic version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildInfo()
			if a.g.json {
				return a.writeJSON(info)
			}
			fmt.Fprintf(a.Stdout, "apic %s %s\n", theme.Accent.Render(info.Version), theme.Dim.Render(fmt.Sprintf("· %s · %s %s/%s", info.Commit, info.Go, info.OS, info.Arch)))
			fmt.Fprintln(a.Stdout, theme.Dim.Render("apic is epic · https://datagriff.github.io/api-caller/"))
			return nil
		},
	}
}

// versionInfo is what `apic version --json` prints.
type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date,omitempty"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

func buildInfo() versionInfo {
	v := versionInfo{Version: Version, Commit: "unknown", Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if len(s.Value) > 7 {
					v.Commit = s.Value[:7]
				} else if s.Value != "" {
					v.Commit = s.Value
				}
			case "vcs.time":
				v.Date = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					v.Commit += "-dirty"
				}
			}
		}
	}
	return v
}

// completeRequests offers request ids and .http files to shell completion.
func (a *App) completeRequests(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	p, err := project.Load(a.g.dir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	seen := map[string]bool{}
	for _, r := range p.Requests() {
		if strings.HasPrefix(r.ID(), toComplete) {
			out = append(out, r.ID()+"\t"+r.Method+" "+r.URL)
		}
		if !seen[r.File.Path] && strings.HasPrefix(r.File.Path, toComplete) {
			seen[r.File.Path] = true
			out = append(out, r.File.Path+"\tevery request in the file")
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeEnvs offers environment names to shell completion.
func (a *App) completeEnvs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	envs, err := env.Load(a.g.dir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, n := range envs.Names() {
		if strings.HasPrefix(n, toComplete) {
			out = append(out, n)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// varMap parses the repeatable --var flag.
func (a *App) varMap() (map[string]string, error) {
	vars := map[string]string{}
	for _, kv := range a.g.vars {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, runner.Usage(runner.CodeFlag, fmt.Sprintf("--var %q must be name=value", kv))
		}
		vars[k] = v
	}
	return vars, nil
}
