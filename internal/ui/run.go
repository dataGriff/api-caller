package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/runner"
)

// resultMsg is one finished request (a single run, or one step of a flow).
type resultMsg struct {
	runID int
	req   *httpfile.Request
	res   *runner.Result
	err   error
}

type editorDoneMsg struct{ err error }

type clearStatusMsg struct{ seq int }

func (m *Model) handleKey(msg Key) Cmd {
	k := m.keys
	switch {
	case m.showHelp:
		if k.Esc.matches(msg) || k.Help.matches(msg) || k.Quit.matches(msg) {
			m.showHelp = false
		}
		return nil
	case m.confirmClear:
		m.confirmClear = false
		if msg.String() == "y" || msg.String() == "Y" {
			return m.clearSession()
		}
		return nil
	case m.filtering:
		switch msg.Type {
		case KeyEnter:
			m.filtering = false
		case KeyEsc:
			m.filtering = false
			m.filter = ""
		case KeyBackspace:
			if r := []rune(m.filter); len(r) > 0 {
				m.filter = string(r[:len(r)-1])
			}
		case KeyCtrlC:
			return m.quit()
		case KeyRune:
			m.filter += string(msg.Rune)
		}
		m.cursor = 0
		m.clampCursor()
		m.selected = m.selectedReq()
		return nil
	}

	switch {
	case k.Quit.matches(msg):
		return m.quit()
	case k.Help.matches(msg):
		m.showHelp = true
	case k.Esc.matches(msg):
		switch {
		case m.inflight != nil:
			m.cancelRun()
			return m.setStatus("run cancelled")
		case m.filter != "":
			m.filter = ""
			m.clampCursor()
			m.selected = m.selectedReq()
		case m.showCurl:
			m.showCurl = false
		}
	case k.Down.matches(msg):
		m.move(1)
	case k.Up.matches(msg):
		m.move(-1)
	case k.Top.matches(msg):
		m.moveTo(true)
	case k.Bottom.matches(msg):
		m.moveTo(false)
	case k.Filter.matches(msg):
		m.filtering = true
	case k.Run.matches(msg):
		if sel := m.selectedReq(); sel != nil {
			return m.startRun([]*httpfile.Request{sel}, false)
		}
	case k.RunFile.matches(msg):
		if reqs := m.fileRequests(); len(reqs) > 0 {
			return m.startRun(reqs, true)
		}
	case k.RunAll.matches(msg):
		if reqs := m.allRequests(); len(reqs) > 0 {
			return m.startRun(reqs, true)
		}
	case k.NextTab.matches(msg):
		m.tab = (m.tab + 1) % tabCount
	case k.PrevTab.matches(msg):
		m.tab = (m.tab + tabCount - 1) % tabCount
	case k.Tab1.matches(msg):
		m.tab = tabPreview
	case k.Tab2.matches(msg):
		m.tab = tabResponse
	case k.Tab3.matches(msg):
		m.tab = tabChecks
	case k.Tab4.matches(msg):
		m.tab = tabSession
	case k.Headers.matches(msg):
		m.showHeaders = !m.showHeaders
		m.tab = tabResponse
	case k.Curl.matches(msg):
		m.showCurl = !m.showCurl
		m.tab = tabResponse
	case k.Open.matches(msg):
		return m.openEditor()
	case k.Env.matches(msg):
		return m.cycleEnv()
	case k.Reload.matches(msg):
		return m.reload()
	case k.Clear.matches(msg):
		if m.inflight != nil {
			return m.setStatus("busy: wait for the run to finish")
		}
		if m.runner.Session == nil {
			return m.setStatus("session disabled by --no-session")
		}
		m.tab = tabSession
		m.confirmClear = true
	case k.LineUp.matches(msg):
		m.vp.scroll(-1)
	case k.LineDown.matches(msg):
		m.vp.scroll(1)
	case k.PageUp.matches(msg):
		m.vp.halfPageUp()
	case k.PageDown.matches(msg):
		m.vp.halfPageDown()
	}
	return nil
}

func (m *Model) quit() Cmd {
	m.cancelRun()
	m.quitting = true
	return Quit
}

// startRun begins a run of one or more requests; only one run at a time.
func (m *Model) startRun(reqs []*httpfile.Request, flow bool) Cmd {
	if m.inflight != nil {
		return m.setStatus("busy: a run is in progress (esc cancels)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.runSeq++
	m.inflight = &runState{id: m.runSeq, reqs: reqs, flow: flow, started: time.Now(), ctx: ctx, cancel: cancel}
	for _, r := range reqs {
		delete(m.results, r)
		delete(m.errs, r)
	}
	m.showCurl = false
	m.tab = tabResponse
	m.selected = reqs[0]
	return m.stepCmd()
}

// stepCmd sends the current request of the run in flight. The request goes
// out on its own goroutine so the UI keeps redrawing while it is in flight.
func (m *Model) stepCmd() Cmd {
	rs := m.inflight
	req := rs.reqs[rs.idx]
	id, ctx, r := rs.id, rs.ctx, m.runner
	return async(func() Msg {
		res, err := r.Run(ctx, req)
		return resultMsg{runID: id, req: req, res: res, err: err}
	})
}

func (m *Model) cancelRun() {
	if m.inflight == nil {
		return
	}
	m.inflight.cancel()
	m.inflight = nil
	m.runSeq++
}

func (m *Model) handleResult(msg resultMsg) Cmd {
	rs := m.inflight
	if rs == nil || msg.runID != rs.id {
		return nil
	}
	if msg.res != nil {
		m.results[msg.req] = msg.res
		m.storeDeps(msg.res)
	}
	if msg.err != nil {
		m.errs[msg.req] = msg.err
	}
	m.selected = msg.req
	m.syncCursorTo(msg.req)
	rs.idx++
	if rs.idx < len(rs.reqs) {
		return m.stepCmd()
	}
	rs.cancel()
	m.inflight = nil
	m.finishRun(rs)
	return nil
}

// storeDeps records the results of the requests `# @ref` ran first, so
// their rows show what happened to them too.
func (m *Model) storeDeps(res *runner.Result) {
	for _, dep := range res.Deps {
		if req := dep.Req(); req != nil {
			m.results[req] = dep
			delete(m.errs, req)
		}
		m.storeDeps(dep)
	}
}

// finishRun refreshes derived state and the summary once a run completes.
func (m *Model) finishRun(rs *runState) {
	m.refreshDescs()
	m.refreshSession()
	var results []*runner.Result
	for _, r := range rs.reqs {
		if res := m.results[r]; res != nil {
			results = append(results, res)
		} else if err := m.errs[r]; err != nil {
			results = append(results, &runner.Result{Request: runner.Resolved{Name: r.Name, File: r.File.Path, Line: r.Line, Method: r.Method, URL: r.URL}, Errors: []string{err.Error()}})
		}
	}
	if !rs.flow && len(results) == 1 {
		res := results[0]
		switch {
		case m.errs[rs.reqs[0]] != nil:
			m.lastSummary = m.theme.Fail.Render("✗ " + rs.reqs[0].ID())
		case res.Response != nil:
			m.lastSummary = fmt.Sprintf("%s %s %s %s", m.theme.Mark(res.OK), rs.reqs[0].ID(),
				m.theme.StatusCode(res.Response.Status), m.theme.Dim.Render(fmt.Sprintf("%d ms", res.Response.DurationMs)))
		}
		return
	}
	m.lastSummary = strings.TrimSpace(output.SummaryLine(m.theme, results))
}

func (m *Model) syncCursorTo(req *httpfile.Request) {
	for i, it := range m.visible() {
		if it.req == req {
			m.cursor = i
			return
		}
	}
}

// cycleEnv switches to the next environment and rebuilds the runner.
func (m *Model) cycleEnv() Cmd {
	if m.inflight != nil {
		return m.setStatus("busy: wait for the run to finish")
	}
	if len(m.envs) < 2 {
		if len(m.envs) == 0 {
			return m.setStatus("no environments: create http-client.env.json")
		}
		return m.setStatus("only one environment: " + m.envs[0])
	}
	next := m.envs[0]
	for i, e := range m.envs {
		if e == m.env {
			next = m.envs[(i+1)%len(m.envs)]
			break
		}
	}
	return m.switchRunner(next, "env "+next)
}

// reload re-reads the project from disk.
func (m *Model) reload() Cmd {
	if m.inflight != nil {
		return m.setStatus("busy: wait for the run to finish")
	}
	return m.switchRunner(m.env, "project reloaded")
}

func (m *Model) switchRunner(env, status string) Cmd {
	if m.cfg.NewRunner == nil {
		return m.setStatus("cannot rebuild the runner here")
	}
	r, err := m.cfg.NewRunner(env)
	if err != nil {
		return m.setStatus(err.Error())
	}
	sel := ""
	if s := m.selectedReq(); s != nil {
		sel = s.ID()
	}
	m.runner = r
	m.env = env
	m.lastSummary = ""
	m.rebuild(sel)
	return m.setStatus(status)
}

func (m *Model) clearSession() Cmd {
	if m.runner.Session == nil {
		return nil
	}
	m.runner.Session.Clear(m.env)
	if err := m.runner.Session.Save(); err != nil {
		return m.setStatus("session: " + err.Error())
	}
	if m.runner.Jar != nil {
		m.runner.Jar.Clear(m.env)
		if err := m.runner.Jar.Save(); err != nil {
			return m.setStatus("cookies: " + err.Error())
		}
	}
	m.refreshSession()
	m.refreshDescs()
	return m.setStatus("session cleared for " + m.envLabel())
}
