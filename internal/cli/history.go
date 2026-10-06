// apic history: the responses a request returned before, and what changed.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/dataGriff/api-caller/internal/history"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
)

// historyTarget is the history of one request in the environment in
// effect.
type historyTarget struct {
	store   *history.Store
	project *project.Project
	env     string // the environment in effect, "default" for none
	name    string // the request's # @name
}

// historyStore opens the project's history for the environment in
// effect: --env, else apic.yaml's env, else "default". It builds no
// runner, so a session or cookie jar apic cannot read never stands in
// the way of reading or clearing the history. Reading needs no
// `history:` in apic.yaml: what an earlier setting recorded stays
// readable.
func (a *App) historyStore() (*history.Store, *project.Project, string, error) {
	p, err := a.loadProject()
	if err != nil {
		return nil, nil, "", err
	}
	env := a.g.env
	if env == "" {
		env = p.Config.Env
	}
	if env == "" {
		env = history.DefaultEnv
	}
	return history.New(p.Root, p.Config.History), p, env, nil
}

// historyFor resolves target to the key a request's history is kept
// under (see runner.HistoryKey). A name with history but no request any
// more (renamed or deleted since) still resolves, so its history can be
// read and cleared; a name several requests share does not, since the
// history of each is kept apart under file#name.
func (a *App) historyFor(target string) (*historyTarget, error) {
	store, p, env, err := a.historyStore()
	if err != nil {
		return nil, err
	}
	reqs, rerr := p.Resolve(target)
	switch {
	case rerr == nil && len(reqs) == 1:
		if reqs[0].Name == "" {
			return nil, runner.Usage(runner.CodeUnknownRequest, fmt.Sprintf("%s has no # @name; history keeps named requests only", target))
		}
		return &historyTarget{store: store, project: p, env: env, name: runner.HistoryKey(p, reqs[0])}, nil
	case rerr == nil:
		return nil, runner.Usage(runner.CodeAmbiguous, fmt.Sprintf("%s names %d requests; pick one with %s#<name>", target, len(reqs), target))
	}
	shared := 0
	for _, r := range p.Requests() {
		if r.Name == target {
			shared++
		}
	}
	if shared > 1 {
		return nil, runner.Usage(runner.CodeAmbiguous, rerr.Error())
	}
	if entries, err := store.List(env, target); err == nil && len(entries) > 0 {
		return &historyTarget{store: store, project: p, env: env, name: target}, nil
	}
	return nil, runner.Usage(runner.CodeUnknownRequest, rerr.Error())
}

// entryCount renders "1 entry" or "3 entries".
func entryCount(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}

// historyOff is the hint for a project that records nothing.
func historyOff(p *project.Project) string {
	if p.Config.History > 0 {
		return ""
	}
	return "history is off: set `history: 20` in apic.yaml to keep the last 20 responses of each request"
}

// entryIndex reads an entry number argument.
func entryIndex(arg string) (int, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 {
		return 0, runner.Usage(runner.CodeFlag, fmt.Sprintf("%q is not an entry number: 1 is the newest", arg))
	}
	return n, nil
}

// historyErr gives a missing entry the code of any other bad argument.
func historyErr(err error) error {
	var re *history.RangeError
	if errors.As(err, &re) {
		return runner.Usage(runner.CodeFlag, err.Error())
	}
	return runner.Usage(runner.CodeSession, "history: "+err.Error())
}

func (a *App) historyCmd() *cobra.Command {
	var show int
	var verbose bool
	cmd := &cobra.Command{
		Use:   "history [request]",
		Short: "List the responses a request returned before (needs history: N in apic.yaml)",
		Long: `List the responses recorded for a request in the current environment,
newest first, or with no request, the requests that have any.

Recording is off until apic.yaml sets history: N, which keeps the last N
responses of each named request per environment in .apic/history. Entries
are stored as apic run --json prints a result: sensitive headers masked,
and everything when the run used --redact.`,
		Example: `  apic history login
  apic history login --show 2
  apic history diff login
  apic history clear login`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if show > 0 {
					return runner.Usage(runner.CodeFlag, "--show needs a request")
				}
				return a.historyRequests()
			}
			t, err := a.historyFor(args[0])
			if err != nil {
				return err
			}
			if show > 0 {
				return a.historyShow(t, show, verbose)
			}
			return a.historyList(t)
		},
	}
	cmd.Flags().IntVar(&show, "show", 0, "print entry N (1 is the newest) as apic run printed it")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "with --show, include the request and response headers")
	cmd.ValidArgsFunction = a.completeRequests

	diff := &cobra.Command{
		Use:   "diff <request> [from] [to]",
		Short: "Show what changed between two responses (default: the last two)",
		Long: `Compare two recorded responses of a request: the status, then the body.
JSON bodies are compared by structure, keys in sorted order and arrays
index by index; other text line by line. Headers are left out, since a
Date or a request id differs every time. Entries are numbered from 1, the
newest; the default compares 2 with 1.`,
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			from, to := 2, 1
			var err error
			if len(args) > 1 {
				if from, err = entryIndex(args[1]); err != nil {
					return err
				}
			}
			if len(args) > 2 {
				if to, err = entryIndex(args[2]); err != nil {
					return err
				}
			}
			t, err := a.historyFor(args[0])
			if err != nil {
				return err
			}
			return a.historyDiff(t, from, to)
		},
	}
	diff.ValidArgsFunction = a.completeRequests
	cmd.AddCommand(diff)

	var all, everyEnv bool
	clear := &cobra.Command{
		Use:   "clear [request]",
		Short: "Forget the history of a request, or of every request with --all",
		Long: `Forget the history of one request in the current environment. With
--all and no request, forget every request's history in the environment;
add --every-env to forget it in every environment.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if everyEnv {
				all = true
			}
			switch {
			case all && len(args) > 0:
				return runner.Usage(runner.CodeFlag, "--all clears every request; give it no request")
			case !all && len(args) == 0:
				return a.historyClearNeedsScope()
			}
			var store *history.Store
			env, name := "", ""
			if len(args) == 1 {
				t, err := a.historyFor(args[0])
				if err != nil {
					return err
				}
				store, env, name = t.store, t.env, t.name
			} else {
				var err error
				if store, _, env, err = a.historyStore(); err != nil {
					return err
				}
			}
			if everyEnv {
				env = "*"
			}
			n, err := store.Clear(env, name)
			if err != nil {
				return historyErr(err)
			}
			if a.g.json {
				return a.writeJSON(struct {
					Cleared string `json:"cleared"`
					Request string `json:"request,omitempty"`
					Entries int    `json:"entries"`
				}{env, name, n})
			}
			fmt.Fprintf(a.Stdout, "cleared %s\n", entryCount(n))
			return nil
		},
	}
	clear.Flags().BoolVar(&all, "all", false, "clear every request's history in the environment")
	clear.Flags().BoolVar(&everyEnv, "every-env", false, "clear every request's history in every environment (implies --all)")
	clear.ValidArgsFunction = a.completeRequests
	cmd.AddCommand(clear)
	return cmd
}

// historyClearNeedsScope refuses a bare `history clear`, which would
// otherwise wipe a whole environment by accident; a project with a
// request named clear is told how to list its history instead.
func (a *App) historyClearNeedsScope() error {
	msg := "name the request whose history to clear, or pass --all for every request in the environment (--every-env for every environment)"
	if p, err := a.loadProject(); err == nil {
		for _, r := range p.Requests() {
			if r.Name == "clear" {
				msg += fmt.Sprintf("; to list the history of the request named clear, use apic history %s#clear", r.File.Path)
				break
			}
		}
	}
	return runner.Usage(runner.CodeFlag, msg)
}

func (a *App) historyRequests() error {
	store, p, env, err := a.historyStore()
	if err != nil {
		return err
	}
	reqs, err := store.Requests(env)
	if err != nil {
		return historyErr(err)
	}
	if a.g.json {
		if reqs == nil {
			reqs = []history.Recorded{}
		}
		return a.writeJSON(struct {
			Env      string             `json:"env"`
			Keep     int                `json:"keep"`
			Requests []history.Recorded `json:"requests"`
		}{env, store.Keep(), reqs})
	}
	if len(reqs) == 0 {
		fmt.Fprintf(a.Stdout, "no history in %s\n", env)
		if hint := historyOff(p); hint != "" {
			fmt.Fprintln(a.Stdout, theme.Dim.Render(hint))
		}
		return nil
	}
	fmt.Fprintln(a.Stdout, theme.Bold.Render(env))
	for _, r := range reqs {
		fmt.Fprintf(a.Stdout, "  %s %s\n", r.Request, theme.Dim.Render(entryCount(r.Entries)))
	}
	return nil
}

func (a *App) historyList(t *historyTarget) error {
	entries, err := t.store.List(t.env, t.name)
	if err != nil {
		return historyErr(err)
	}
	if a.g.json {
		return a.writeJSON(struct {
			Env     string          `json:"env"`
			Request string          `json:"request"`
			Keep    int             `json:"keep"`
			Entries []history.Entry `json:"entries"`
		}{t.env, t.name, t.store.Keep(), entries})
	}
	if len(entries) == 0 {
		fmt.Fprintf(a.Stdout, "no history for %s in %s\n", t.name, t.env)
		if hint := historyOff(t.project); hint != "" {
			fmt.Fprintln(a.Stdout, theme.Dim.Render(hint))
		}
		return nil
	}
	fmt.Fprintf(a.Stdout, "%s %s\n", theme.Bold.Render(t.name), theme.Dim.Render("· "+t.env+" · "+entryCount(len(entries))))
	fmt.Fprint(a.Stdout, output.HistoryEntries(theme, entries))
	return nil
}

func (a *App) historyShow(t *historyTarget, index int, verbose bool) error {
	e, err := t.store.Get(t.env, t.name, index)
	if err != nil {
		return historyErr(err)
	}
	if a.g.json {
		return a.writeJSON(struct {
			history.Entry
			Result json.RawMessage `json:"result"`
		}{e, e.Result})
	}
	res, err := runner.ParseResult(e.Result)
	if err != nil {
		return historyErr(fmt.Errorf("%s: %w", e.File, err))
	}
	fmt.Fprintln(a.Stdout, theme.Dim.Render(fmt.Sprintf("%s · %s · %s", t.name, t.env, output.HistoryEntry(e))))
	output.Human(a.Stdout, res, verbose)
	return nil
}

func (a *App) historyDiff(t *historyTarget, from, to int) error {
	fe, err := t.store.Get(t.env, t.name, from)
	var re *history.RangeError
	if errors.As(err, &re) && re.Have < 2 {
		return runner.Usage(runner.CodeFlag, fmt.Sprintf("%s has %s in %s; a diff needs two (run it again)", t.name, entryCount(re.Have), t.env))
	}
	if err != nil {
		return historyErr(err)
	}
	te, err := t.store.Get(t.env, t.name, to)
	if err != nil {
		return historyErr(err)
	}
	changes, err := history.Compare(fe.Result, te.Result)
	if err != nil {
		return historyErr(err)
	}
	if a.g.json {
		if changes == nil {
			changes = []history.Change{}
		}
		return a.writeJSON(struct {
			Env     string           `json:"env"`
			Request string           `json:"request"`
			From    history.Entry    `json:"from"`
			To      history.Entry    `json:"to"`
			Changes []history.Change `json:"changes"`
		}{t.env, t.name, fe, te, changes})
	}
	fmt.Fprintf(a.Stdout, "%s %s\n", theme.Bold.Render(t.name), theme.Dim.Render(output.HistoryEntry(fe)+" → "+output.HistoryEntry(te)))
	if len(changes) == 0 {
		fmt.Fprintln(a.Stdout, "no changes in status or body")
		return nil
	}
	fmt.Fprint(a.Stdout, output.Changes(theme, changes, 80))
	fmt.Fprintln(a.Stdout, theme.Dim.Render(plural(len(changes), "change")))
	return nil
}
