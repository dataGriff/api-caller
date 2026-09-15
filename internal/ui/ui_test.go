package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/dataGriff/api-caller/internal/demoapi"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
	"github.com/dataGriff/api-caller/internal/session"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}

type fixture struct {
	dir     string
	srv     *httptest.Server
	m       *Model
	rebuilt []string
}

// newFixture serves the demo API, writes the demo project against it, and
// builds a sized model on top.
func newFixture(t *testing.T, opts runner.Options) *fixture {
	t.Helper()
	srv := httptest.NewServer(demoapi.New())
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	if _, _, err := demoapi.WriteProject(dir, 1, true); err != nil {
		t.Fatal(err)
	}
	env := fmt.Sprintf("{\n  \"local\": {\"baseUrl\": %q},\n  \"other\": {\"baseUrl\": %q}\n}\n", srv.URL, srv.URL)
	if err := os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fixture{dir: dir, srv: srv}
	build := func(envName string) (*runner.Runner, error) {
		p, err := project.Load(dir)
		if err != nil {
			return nil, err
		}
		o := opts
		o.Env = envName
		if o.Session == nil {
			o.Session = session.NewMemory()
		}
		f.rebuilt = append(f.rebuilt, envName)
		return runner.New(p, o)
	}
	r, err := build("local")
	if err != nil {
		t.Fatal(err)
	}
	f.rebuilt = nil
	f.m = New(Config{Root: dir, Env: "local", Runner: r, NewRunner: build, Editor: "vim", Redact: opts.Redact, Theme: output.Default()})
	f.m.Update(sizeMsg{width: 120, height: 40})
	return f
}

func kp(s string) Key { return ParseKey(s) }

func (f *fixture) press(keys ...string) Cmd {
	var cmd Cmd
	for _, k := range keys {
		cmd = f.m.Update(kp(k))
	}
	return cmd
}

// step resolves one command: work scheduled off the loop is run inline and
// its message fed back to the model. It returns the next command, if any.
func (f *fixture) step(cmd Cmd) Cmd {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case nil, quitMsg:
		return nil
	case asyncMsg:
		return f.m.Update(msg.run())
	default:
		return f.m.Update(msg)
	}
}

// drain resolves a whole command chain, such as a flow running request by
// request.
func (f *fixture) drain(cmd Cmd) { f.m.settle(cmd) }

func (f *fixture) view() string { return f.m.View() }

func TestInitIsInertAndListIsGrouped(t *testing.T) {
	f := newFixture(t, runner.Options{})
	v := f.view()
	for _, want := range []string{"auth.http", "todos.http", "POST", "login", "whoami", "list-todos", "1 preview", "env=local", "▸"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in view:\n%s", want, v)
		}
	}
	if strings.Contains(v, "\x1b[") {
		t.Error("view should have no escape codes under the Ascii profile")
	}
	if f.m.Selected() == nil || f.m.Selected().ID() != "login" {
		t.Fatalf("first request should be selected, got %v", f.m.Selected())
	}
}

func TestTooSmall(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.m.Update(sizeMsg{width: 40, height: 10})
	if !strings.Contains(f.view(), "terminal too small") {
		t.Fatalf("got %q", f.view())
	}
}

func TestNavigationSkipsFileHeadings(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.press("j", "j", "j")
	if f.m.Selected().ID() != "oauth2-token" {
		t.Fatalf("after 3 downs got %s", f.m.Selected().ID())
	}
	f.press("j")
	if f.m.Selected().ID() != "list-todos" {
		t.Fatalf("moving past a file heading should land on the next request, got %s", f.m.Selected().ID())
	}
	f.press("k")
	if f.m.Selected().ID() != "oauth2-token" {
		t.Fatalf("up got %s", f.m.Selected().ID())
	}
	f.press("G")
	if f.m.Selected().ID() != "redirect-raw" {
		t.Fatalf("G got %s", f.m.Selected().ID())
	}
	f.press("g")
	if f.m.Selected().ID() != "login" {
		t.Fatalf("g got %s", f.m.Selected().ID())
	}
}

func TestFilter(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.press("/", "t", "o", "d", "o", "enter")
	v := f.view()
	if strings.Contains(v, "POST   login") || !strings.Contains(v, "list-todos") || !strings.Contains(v, "/todo") {
		t.Fatalf("filter should hide login and keep todos:\n%s", v)
	}
	if f.m.Selected().ID() != "list-todos" {
		t.Fatalf("cursor should move to the first match, got %s", f.m.Selected().ID())
	}
	f.press("esc")
	if !strings.Contains(f.view(), "POST   login") {
		t.Fatal("esc should clear the filter")
	}
}

func TestRunSelectedAndBusy(t *testing.T) {
	f := newFixture(t, runner.Options{})
	cmd := f.press("enter")
	if !f.m.Running() || cmd == nil {
		t.Fatal("enter should start a run")
	}
	if second := f.press("enter"); second == nil || !strings.Contains(f.m.Status(), "busy") {
		t.Fatalf("a second enter while running should report busy, status=%q", f.m.Status())
	}
	f.drain(cmd)
	if f.m.Running() {
		t.Fatal("run should have finished")
	}
	res := f.m.Result(f.m.Selected())
	if res == nil || !res.OK || res.Response.Status != 200 {
		t.Fatalf("login should pass: %+v", res)
	}
	v := f.view()
	for _, want := range []string{"200 OK", "access_token", "mock-token", "✓ status == 200", "↳ token = mock-token", "last: ✓ login"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in view:\n%s", want, v)
		}
	}
	f.press("3")
	if v := f.view(); !strings.Contains(v, "3 checks") || !strings.Contains(v, "✓ status == 200") {
		t.Errorf("checks tab:\n%s", v)
	}
	f.press("4")
	if v := f.view(); !strings.Contains(v, "↳ token = mock-token") {
		t.Errorf("session tab should show the capture:\n%s", v)
	}
}

func TestFlowRunsWholeFileLive(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.press("j") // whoami
	cmd := f.press("f")
	if !f.m.Running() {
		t.Fatal("f should start a flow")
	}
	// Resolve just the first step and check the list shows it before the rest run.
	first := f.step(cmd)
	v := f.view()
	if !strings.Contains(v, "✓ POST   login") || !strings.Contains(v, "running whoami (2/4)") {
		t.Fatalf("after the first step the list should show login passed and whoami running:\n%s", v)
	}
	f.drain(first)
	if f.m.Running() {
		t.Fatal("flow should finish")
	}
	if v := f.view(); !strings.Contains(v, "last: 4 passed") {
		t.Fatalf("summary missing:\n%s", v)
	}
}

func TestRunAllReportsFailures(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.drain(f.press("a"))
	v := f.view()
	if !strings.Contains(v, "1 failed, 13 passed") || !strings.Contains(v, "✗ GET    not-found") {
		t.Fatalf("expected the deliberate failure in the summary and list:\n%s", v)
	}
}

func TestStaleResultDropped(t *testing.T) {
	f := newFixture(t, runner.Options{})
	cmd := f.press("enter")
	f.press("esc")
	if f.m.Running() || !strings.Contains(f.m.Status(), "cancelled") {
		t.Fatalf("esc should cancel, status=%q", f.m.Status())
	}
	if async, ok := cmd().(asyncMsg); ok {
		f.m.Update(async.run())
	} else {
		t.Fatal("a run should be scheduled off the loop")
	}
	if f.m.Result(f.m.Selected()) != nil {
		t.Fatal("a result from a cancelled run must not be recorded")
	}
}

func TestSessionClearNeedsConfirm(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.drain(f.press("enter"))
	f.press("4", "x")
	if v := f.view(); !strings.Contains(v, `clear the session for "local"? (y/n)`) {
		t.Fatalf("expected a confirmation:\n%s", v)
	}
	f.press("n")
	if v := f.view(); !strings.Contains(v, "↳ token") {
		t.Fatal("n should keep the session")
	}
	f.press("x", "y")
	if v := f.view(); strings.Contains(v, "↳ token") || !strings.Contains(v, "nothing captured yet") {
		t.Fatalf("y should clear the session:\n%s", v)
	}
}

func TestHeadersAndCurlToggles(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.drain(f.press("enter"))
	f.press("H")
	if v := f.view(); !strings.Contains(v, "content-type: application/json") || !strings.Contains(v, "Content-Type: application/json") {
		t.Fatalf("H should show request and response headers:\n%s", v)
	}
	f.press("c")
	if v := f.view(); !strings.Contains(v, "curl -sS") || !strings.Contains(v, "/auth/login'") {
		t.Fatalf("c should show the curl command:\n%s", v)
	}
	f.press("c")
	if v := f.view(); strings.Contains(v, "curl -sS") {
		t.Fatal("c again should hide curl")
	}
}

func TestEnvCycleRebuildsRunner(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.drain(f.press("enter"))
	f.press("e")
	if f.m.Env() != "other" || len(f.rebuilt) != 1 || f.rebuilt[0] != "other" {
		t.Fatalf("env=%s rebuilt=%v", f.m.Env(), f.rebuilt)
	}
	if v := f.view(); !strings.Contains(v, "env=other") || strings.Contains(v, "200 OK") {
		t.Fatalf("status bar should show the new env and results should be cleared:\n%s", v)
	}
	f.press("e")
	if f.m.Env() != "local" {
		t.Fatalf("cycling wraps around, got %s", f.m.Env())
	}
}

func TestReloadPicksUpNewFile(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.press("j", "j")
	if err := os.WriteFile(filepath.Join(f.dir, "extra.http"), []byte("### Extra\n# @name extra\nGET {{baseUrl}}/status/200\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.press("r")
	if v := f.view(); !strings.Contains(v, "extra.http") || !strings.Contains(v, "project reloaded") {
		t.Fatalf("reload should list the new file:\n%s", v)
	}
	if f.m.Selected().ID() != "basic-auth" {
		t.Fatalf("reload should keep the cursor on the same request, got %s", f.m.Selected().ID())
	}
}

func TestEditorCommand(t *testing.T) {
	cmd, err := editorCommand("vim", "auth.http", 7)
	if err != nil || strings.Join(cmd.Args[1:], " ") != "+7 auth.http" {
		t.Fatalf("vim: %v %v", cmd.Args, err)
	}
	cmd, err = editorCommand("code --wait", "auth.http", 7)
	if err != nil || strings.Join(cmd.Args[1:], " ") != "--wait -g auth.http:7" {
		t.Fatalf("code: %v %v", cmd.Args, err)
	}
	if _, err := editorCommand("", "x", 1); err == nil {
		t.Fatal("empty editor should error")
	}
	f := newFixture(t, runner.Options{})
	f.m.cfg.Editor = ""
	f.press("o")
	if !strings.Contains(f.m.Status(), "$EDITOR") {
		t.Fatalf("status=%q", f.m.Status())
	}
}

func TestQuitCancelsInflight(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-blocked:
		case <-r.Context().Done():
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	defer close(blocked)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "slow.http"), []byte("### Slow\n# @name slow\nGET "+srv.URL+"/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(p, runner.Options{Session: session.NewMemory()})
	if err != nil {
		t.Fatal(err)
	}
	m := New(Config{Runner: r, Theme: output.Default()})
	m.Update(sizeMsg{width: 100, height: 30})
	cmd := m.Update(kp("enter"))
	ctx := m.inflight.ctx
	async, ok := cmd().(asyncMsg)
	if !ok {
		t.Fatal("a run should be scheduled off the loop")
	}
	done := make(chan Msg, 1)
	go func() { done <- async.run() }()
	quit := m.Update(kp("q"))
	if quit == nil {
		t.Fatal("q should return a command")
	}
	if _, ok := quit().(quitMsg); !ok {
		t.Fatal("q should quit")
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("quit should cancel the in-flight request, err=%v", ctx.Err())
	}
	<-done
}

func TestHelpOverlay(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.press("?")
	if v := f.view(); !strings.Contains(v, "run selected") || !strings.Contains(v, "apic is epic") {
		t.Fatalf("help missing:\n%s", v)
	}
	f.press("esc")
	if strings.Contains(f.view(), "run selected") {
		t.Fatal("esc should close help")
	}
}

func TestRedactMasksValues(t *testing.T) {
	f := newFixture(t, runner.Options{Redact: true})
	f.drain(f.press("enter"))
	f.press("H")
	v := f.view()
	if strings.Contains(v, "s3cret") || !strings.Contains(v, "***") {
		t.Fatalf("redact should mask the request body:\n%s", v)
	}
	f.press("4")
	if v := f.view(); strings.Contains(v, "mock-token") {
		t.Fatalf("redact should mask session values:\n%s", v)
	}
}

func TestPreviewShowsMissingVariableHint(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.press("j") // whoami needs {{token}}
	v := f.view()
	if !strings.Contains(v, "not ready") || !strings.Contains(v, "captured by login") {
		t.Fatalf("preview should explain the missing token:\n%s", v)
	}
	if !strings.Contains(v, "○ GET    whoami") {
		t.Fatalf("list should mark whoami as not ready:\n%s", v)
	}
}

func TestPaneScrollSurvivesRedraw(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.m.Update(sizeMsg{width: 100, height: 24})
	f.drain(f.press("enter"))
	f.press("1") // the preview is taller than the pane
	f.view()
	if len(f.m.vp.lines) <= f.m.vp.height {
		t.Skipf("preview fits in %d lines; nothing to scroll", f.m.vp.height)
	}
	f.m.Update(Key{Type: KeyCtrlD})
	want := f.m.vp.offset
	if want == 0 {
		t.Fatal("ctrl+d should scroll the pane down")
	}
	if f.view(); f.m.vp.offset != want {
		t.Fatalf("redrawing reset the scroll offset to %d, want %d", f.m.vp.offset, want)
	}
	f.press("K")
	if f.m.vp.offset != want-1 {
		t.Fatalf("K should scroll up a line, offset=%d want %d", f.m.vp.offset, want-1)
	}
	f.press("J")
	if f.m.vp.offset != want {
		t.Fatalf("J should scroll down a line, offset=%d want %d", f.m.vp.offset, want)
	}
	f.press("2") // another tab is another subject: back to the top
	f.view()
	if f.m.vp.offset != 0 {
		t.Fatalf("switching tab should scroll back to the top, offset=%d", f.m.vp.offset)
	}
}

func TestScrollPositionShowsWhereThePaneIs(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.m.Update(sizeMsg{width: 100, height: 24})
	f.drain(f.press("enter"))
	f.press("1")
	if v := f.view(); !strings.Contains(v, "top ↓") {
		t.Fatalf("a pane with more below should say so:\n%s", v)
	}
	for i := 0; i < 20; i++ {
		f.m.Update(Key{Type: KeyCtrlD})
	}
	if v := f.view(); !strings.Contains(v, "↑ end") {
		t.Fatalf("a pane scrolled to the bottom should say so:\n%s", v)
	}
	f.m.Update(sizeMsg{width: 100, height: 90}) // now everything fits
	if v := f.view(); strings.Contains(v, "top ↓") || strings.Contains(v, "↑ end") {
		t.Fatalf("content that fits needs no scroll position:\n%s", v)
	}
}

func TestRowsCarryTheOutcomeAndFilesRollUp(t *testing.T) {
	f := newFixture(t, runner.Options{})
	f.m.Update(sizeMsg{width: 120, height: 30})
	if v := f.view(); !strings.Contains(v, "auth.http") || !strings.Contains(v, "todos.http") {
		t.Fatalf("the list should be grouped by file:\n%s", v)
	}
	f.drain(f.press("f")) // run auth.http as a flow
	v := f.view()
	if !strings.Contains(v, "200 ") {
		t.Fatalf("a request that ran should show its status:\n%s", v)
	}
	if !strings.Contains(v, "✓4") {
		t.Fatalf("auth.http should roll up four passes:\n%s", v)
	}
	if !strings.Contains(v, "last: 4 passed") {
		t.Fatalf("status bar should summarise the flow:\n%s", v)
	}
}

func TestStatusBarBadges(t *testing.T) {
	f := newFixture(t, runner.Options{Redact: true})
	f.m.cfg.Demo = true
	v := f.view()
	if !strings.Contains(v, "demo") || !strings.Contains(v, "redact") {
		t.Fatalf("status bar should badge demo and redact:\n%s", v)
	}
}

func TestShortDuration(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{120 * time.Millisecond, "120ms"},
		{999 * time.Millisecond, "999ms"},
		{1500 * time.Millisecond, "1.5s"},
		{12 * time.Second, "12.0s"},
	} {
		if got := shortDuration(tc.d); got != tc.want {
			t.Errorf("shortDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
