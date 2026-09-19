package cli

import (
	"context"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/demoapi"
	"github.com/dataGriff/api-caller/internal/runner"
	"github.com/dataGriff/api-caller/internal/ui"
)

func (a *App) uiCmd() *cobra.Command {
	var demo bool
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Browse and run the project's requests in a terminal UI",
		Long: `ui opens an interactive terminal UI: the requests on the left, and a
preview, the response, the checks and the session on the right. Run a
request with enter, a whole file with f, switch environment with e, and
press ? for every key.

This is the one apic command that needs a terminal. It refuses to start when
stdout is not a TTY or --json is given; use apic run / apic list there.

Like every apic command, ui resolves .http files relative to -C (default:
the current directory) — it has no project registry, so it won't find a
project written elsewhere unless you cd into it or pass -C.

--demo needs nothing set up: it serves the built-in fake API in-process and
opens the UI on the example project that targets it.`,
		Example: `  apic ui
  apic ui -C api --env staging
  apic ui --demo`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.g.json {
				return &runner.UsageError{Msg: "apic ui is interactive and does not support --json; use apic run or apic list --json"}
			}
			if !isTerminal(a.Stdout) {
				return &runner.UsageError{Msg: "apic ui needs an interactive terminal (stdout is not a TTY); use apic run or apic list"}
			}
			if demo {
				root, stop, err := startDemo()
				if err != nil {
					return err
				}
				defer stop()
				a.g.dir = root
			}
			r, err := a.newRunner()
			if err != nil {
				return err
			}
			cfg := ui.Config{
				Root:   r.Project.Root,
				Env:    r.Opts.Env,
				Runner: r,
				NewRunner: func(env string) (*runner.Runner, error) {
					a.g.env = env
					return a.newRunner()
				},
				Editor: editorCommand(),
				Redact: a.g.redact,
				Demo:   demo,
				Theme:  theme,
			}
			stdin, ok := a.Stdin.(*os.File)
			if !ok {
				return &runner.UsageError{Msg: "apic ui needs an interactive terminal"}
			}
			if err := ui.Run(cmd.Context(), ui.New(cfg), stdin, a.Stdout); err != nil {
				return &runner.UsageError{Msg: "ui: " + err.Error()}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&demo, "demo", false, "serve the built-in demo API in-process and open the UI on its example project")
	return cmd
}

// editorCommand picks the editor for the o key: $VISUAL, then $EDITOR, then
// a platform default.
func editorCommand() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// startDemo writes the example project to a temp dir, serves the fake API
// on a free localhost port, and returns the project root and a stop func.
func startDemo() (root string, stop func(), err error) {
	dir, err := os.MkdirTemp("", "apic-ui-demo-*")
	if err != nil {
		return "", nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if _, _, err := demoapi.WriteProject(dir, port, true); err != nil {
		_ = ln.Close()
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	srv := &http.Server{Handler: demoapi.New(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	stop = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = os.RemoveAll(dir)
	}
	return dir, stop, nil
}
