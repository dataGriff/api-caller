// Package ui is the interactive terminal UI behind `apic ui`: a request list
// on the left, a tabbed detail pane on the right, and a status bar. It is a
// Bubble Tea model driven entirely by messages, so it is unit-testable
// without a terminal.
package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/runner"
)

// Config is everything the CLI hands the UI; tests build it directly.
type Config struct {
	Root      string                                   // project root shown in the status bar
	Env       string                                   // initial environment
	Runner    *runner.Runner                           // initial runner
	NewRunner func(env string) (*runner.Runner, error) // rebuilds the runner on env change or reload
	Editor    string                                   // command for the o key; empty disables it
	Redact    bool                                     // --redact
	Demo      bool                                     // shows a demo badge in the status bar
	Theme     output.Theme
}

const (
	minWidth  = 70
	minHeight = 16
)

type tab int

const (
	tabPreview tab = iota
	tabResponse
	tabChecks
	tabSession
	tabCount
)

var tabNames = [tabCount]string{"preview", "response", "checks", "session"}

// runState is the run in flight, if any.
type runState struct {
	id     int
	reqs   []*httpfile.Request
	idx    int
	flow   bool
	ctx    context.Context
	cancel context.CancelFunc
}

// Model is the Bubble Tea model.
type Model struct {
	cfg    Config
	theme  output.Theme
	runner *runner.Runner
	envs   []string
	env    string

	items     []item
	cursor    int
	filter    string
	filtering bool

	descs   map[*httpfile.Request]*runner.Description
	results map[*httpfile.Request]*runner.Result
	errs    map[*httpfile.Request]error
	session map[string]string

	inflight *runState
	runSeq   int

	selected    *httpfile.Request
	tab         tab
	showHeaders bool
	showCurl    bool
	vp          viewport
	paneKey     string

	width, height int
	spin          spinner
	keys          keyMap
	showHelp      bool
	confirmClear  bool
	status        string
	statusSeq     int
	lastSummary   string
	quitting      bool
}

// New builds the model from a config.
func New(cfg Config) *Model {
	m := &Model{
		cfg:    cfg,
		theme:  cfg.Theme,
		runner: cfg.Runner,
		env:    cfg.Env,
		spin:   newSpinner(),
		keys:   newKeyMap(),
	}
	m.rebuild("")
	return m
}

// rebuild refreshes everything derived from the runner: the request rows,
// environments, descriptions and the session snapshot. selectID keeps the
// cursor on the same request across a reload when possible.
func (m *Model) rebuild(selectID string) {
	m.envs = m.runner.Envs.Names()
	if m.env == "" {
		m.env = m.runner.Opts.Env
	}
	m.items = buildItems(m.runner.Project.Requests())
	m.results = map[*httpfile.Request]*runner.Result{}
	m.errs = map[*httpfile.Request]error{}
	m.selected = nil
	m.refreshDescs()
	m.refreshSession()
	m.cursor = 0
	m.clampCursor()
	if selectID != "" {
		for i, it := range m.visible() {
			if it.req != nil && it.req.ID() == selectID {
				m.cursor = i
				break
			}
		}
	}
	m.selected = m.selectedReq()
	m.paneKey = ""
}

func (m *Model) refreshDescs() {
	m.descs = map[*httpfile.Request]*runner.Description{}
	for _, it := range m.items {
		if it.req != nil {
			m.descs[it.req] = m.runner.Describe(it.req)
		}
	}
}

func (m *Model) refreshSession() {
	m.session = map[string]string{}
	if m.runner.Session == nil {
		return
	}
	for k, v := range m.runner.Session.Vars(m.env) {
		m.session[k] = v
	}
}

// Update handles one message and returns any follow-up work.
func (m *Model) Update(msg Msg) Cmd {
	switch msg := msg.(type) {
	case sizeMsg:
		m.width, m.height = msg.width, msg.height
		m.resize()
	case tickMsg:
		if m.inflight != nil {
			m.spin.tick()
			m.paneKey = ""
		}
	case resultMsg:
		return m.handleResult(msg)
	case editorDoneMsg:
		if msg.err != nil {
			return m.setStatus("editor: " + msg.err.Error())
		}
		return m.reload()
	case clearStatusMsg:
		if msg.seq == m.statusSeq {
			m.status = ""
		}
	case Key:
		return m.handleKey(msg)
	}
	return nil
}

// View renders the whole screen.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("terminal too small: apic ui needs %dx%d, have %dx%d", minWidth, minHeight, m.width, m.height)
	}
	m.syncPane()
	leftW, rightW := m.paneWidths()
	contentH := m.height - 1
	left := m.renderList(leftW, contentH)
	right := m.renderTabs(rightW) + "\n" + m.vp.view()
	sep := strings.TrimRight(strings.Repeat(m.theme.Dim.Render("│")+"\n", contentH), "\n")
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
	return body + "\n" + m.renderStatus()
}

func (m *Model) paneWidths() (int, int) {
	left := m.width / 3
	if left < 28 {
		left = 28
	}
	if left > 44 {
		left = 44
	}
	return left, m.width - left - 1
}

func (m *Model) resize() {
	if m.width < minWidth || m.height < minHeight {
		return
	}
	_, rightW := m.paneWidths()
	m.vp.width = rightW
	m.vp.height = m.height - 2
	m.paneKey = ""
}

// syncPane refreshes the viewport content when what it shows has changed.
func (m *Model) syncPane() {
	key := m.paneID()
	if key == m.paneKey {
		return
	}
	changedReq := !strings.HasPrefix(m.paneKey, m.paneReqID()+"|")
	m.paneKey = key
	content := m.renderPane(m.vp.width)
	m.vp.setContent(ansi.Hardwrap(content, m.vp.width, true))
	if changedReq {
		m.vp.gotoTop()
	}
}

func (m *Model) paneReqID() string {
	if m.selected == nil {
		return ""
	}
	return m.selected.ID()
}

func (m *Model) paneID() string {
	var res, desc any
	if m.selected != nil {
		res = m.results[m.selected]
		desc = m.descs[m.selected]
	}
	return fmt.Sprintf("%s|%d|%v|%v|%v|%v|%p|%p|%d|%d|%d|%d", m.paneReqID(), m.tab, m.showHeaders, m.showCurl, m.showHelp, m.confirmClear, res, desc, len(m.session), m.vp.width, m.vp.height, m.spin.i)
}

// renderStatus draws the bottom bar.
func (m *Model) renderStatus() string {
	t := m.theme
	parts := []string{t.Accent.Render("apic")}
	if m.cfg.Demo {
		parts = append(parts, t.Warn.Render("demo"))
	}
	parts = append(parts, t.Dim.Render(output.Truncate(m.cfg.Root, 30)))
	env := m.env
	if env == "" {
		env = "(none)"
	}
	parts = append(parts, "env="+t.Bold.Render(env))
	if m.inflight != nil {
		rs := m.inflight
		name := rs.reqs[rs.idx].ID()
		if rs.flow {
			parts = append(parts, m.theme.Accent.Render(m.spin.view())+" running "+name+fmt.Sprintf(" (%d/%d)", rs.idx+1, len(rs.reqs)))
		} else {
			parts = append(parts, m.theme.Accent.Render(m.spin.view())+" running "+name)
		}
	} else if m.lastSummary != "" {
		parts = append(parts, "last: "+m.lastSummary)
	}
	if m.status != "" {
		parts = append(parts, t.Warn.Render(m.status))
	}
	parts = append(parts, t.Dim.Render("? help · q quit"))
	line := strings.Join(parts, t.Dim.Render(" · "))
	return ansi.Truncate(line, m.width, "…")
}

// setStatus shows a transient message in the status bar for a few seconds.
func (m *Model) setStatus(s string) Cmd {
	m.status = s
	m.statusSeq++
	return after(4*time.Second, clearStatusMsg{seq: m.statusSeq})
}

// Selected returns the request under the cursor (for tests).
func (m *Model) Selected() *httpfile.Request { return m.selectedReq() }

// Env returns the current environment (for tests).
func (m *Model) Env() string { return m.env }

// Running reports whether a run is in flight (for tests).
func (m *Model) Running() bool { return m.inflight != nil }

// Result returns the recorded result for a request (for tests).
func (m *Model) Result(req *httpfile.Request) *runner.Result { return m.results[req] }

// Status returns the transient status text (for tests).
func (m *Model) Status() string { return m.status }

func sortedKeys(mm map[string]string) []string {
	keys := make([]string, 0, len(mm))
	for k := range mm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
