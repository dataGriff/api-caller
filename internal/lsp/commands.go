package lsp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/output"
	"github.com/dataGriff/api-caller/internal/runner"
	"github.com/dataGriff/api-caller/internal/snippet"
)

// codeLenses puts Run, Describe and curl above every request, as commands
// the server itself executes, so a client with no apic-specific UI still
// gets them.
func (s *server) codeLenses(uri string) []codeLens {
	path, ok := uriToPath(uri)
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := fileOf(s.projectFor(path), path)
	if f == nil {
		return []codeLens{}
	}
	out := []codeLens{}
	for _, r := range f.Requests {
		at := lspRange{Start: position{Line: r.Line - 1}, End: position{Line: r.Line - 1}}
		args := []any{uri, target(r)}
		run := "▶ Run"
		if r.Name != "" {
			run += " " + r.Name
		}
		out = append(out,
			codeLens{Range: at, Command: &command{Title: run, Command: CommandRun, Arguments: args}},
			codeLens{Range: at, Command: &command{Title: "Describe", Command: CommandDescribe, Arguments: args}},
			codeLens{Range: at, Command: &command{Title: "curl", Command: CommandCurl, Arguments: args}})
	}
	return out
}

// format rewrites a document in apic fmt's canonical form, as one edit
// over the whole text; no edits when it is already canonical.
func (s *server) format(uri string) []textEdit {
	path, ok := uriToPath(uri)
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	text := s.text(path)
	formatted := httpfile.Format(text)
	if formatted == text {
		return []textEdit{}
	}
	return []textEdit{{Range: lspRange{End: s.units.endOf(text)}, NewText: formatted}}
}

// deferred is a reply a command sends itself once it finishes.
type deferred struct{}

// execute runs a code lens command. Run sends the request, on its own
// goroutine so the editor keeps its completions meanwhile, and answers
// with the result `apic run --json` prints; Describe answers with what
// `apic describe --json` prints, and curl with the command. Each also
// shows a one-line summary and writes the full text report to the
// client's log.
func (s *server) execute(id *json.RawMessage, p executeCommandParams) (any, error) {
	if p.Command == CommandValidate {
		s.validateWorkspace()
		return nil, nil
	}
	if len(p.Arguments) < 2 {
		return nil, &rpcError{Code: codeInvalidParams, Message: p.Command + " takes a document URI and a request target"}
	}
	var uri, tgt string
	if json.Unmarshal(p.Arguments[0], &uri) != nil || json.Unmarshal(p.Arguments[1], &tgt) != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: p.Command + " takes a document URI and a request target, both strings"}
	}
	path, ok := uriToPath(uri)
	if !ok {
		return nil, &rpcError{Code: codeInvalidParams, Message: "not a file URI: " + uri}
	}
	switch p.Command {
	case CommandRun, CommandDescribe, CommandCurl:
	default:
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown command " + p.Command}
	}
	s.mu.Lock()
	st := s.projectFor(path)
	env := s.env
	s.mu.Unlock()
	if st.err != nil {
		return nil, st.err
	}
	reqs, err := st.p.Resolve(tgt)
	if err != nil {
		return nil, err
	}
	if len(reqs) != 1 {
		return nil, fmt.Errorf("%s names %d requests", tgt, len(reqs))
	}
	req := reqs[0]
	switch p.Command {
	case CommandDescribe:
		s.mu.Lock()
		r, err := s.runnerFor(st)
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		d := r.Describe(req)
		for i := range d.Variables {
			if d.Variables[i].Secret {
				d.Variables[i].Value = runner.Masked
			}
		}
		s.log(output.Describe(output.Default(), d, req.Headers))
		s.show(messageInfo, describeSummary(req, d))
		return d, nil
	case CommandCurl:
		s.mu.Lock()
		r, err := s.runnerFor(st)
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if d := r.Describe(req); !d.Ready {
			return nil, fmt.Errorf("%s", describeSummary(req, d))
		}
		resolved, err := r.Resolve(req)
		if err != nil {
			return nil, err
		}
		code, err := snippet.Render("curl", resolved, false)
		if err != nil {
			return nil, err
		}
		s.log(code)
		s.show(messageInfo, code)
		return code, nil
	}
	// The run itself: a real runner, with the session and the history,
	// as `apic run` has. It refuses a project with errors, as the CLI does.
	r, err := runner.New(st.p, runner.Options{Env: env})
	if err != nil {
		return nil, err
	}
	s.startRun(id, r, req, st.root)
	return deferred{}, nil
}

// startRun sends the request on its own goroutine and answers the
// executeCommand request id when it is done.
func (s *server) startRun(id *json.RawMessage, r *runner.Runner, req *httpfile.Request, root string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		res, err := r.Run(s.runCtx, req)
		if err != nil {
			s.show(messageError, fmt.Sprintf("✗ %s: %v", req.ID(), err))
			_ = s.conn.reply(id, nil, err)
			return
		}
		s.remember(r, root, res)
		var b strings.Builder
		output.Human(&b, res, false)
		s.log(b.String())
		kind := messageInfo
		if !res.OK {
			kind = messageWarning
		}
		s.show(kind, runSummary(res))
		_ = s.conn.reply(id, res, nil)
	}()
}

// remember keeps the JSON bodies a run received, for body.$ completion,
// the request's and those its # @ref ran first.
func (s *server) remember(r *runner.Runner, root string, res *runner.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var walk func(*runner.Result)
	walk = func(res *runner.Result) {
		if req := res.Req(); req != nil && req.Name != "" && res.Response != nil {
			if body, ok := jsonBody(res.Response.Body); ok {
				s.bodies[root+"\x00"+runner.HistoryKey(r.Project, req)] = body
			}
		}
		for _, d := range res.Deps {
			walk(d)
		}
	}
	walk(res)
}

// runSummary is the one line a run leaves in the editor:
// "✓ login · 200 OK · 87 ms", or what failed.
func runSummary(res *runner.Result) string {
	name := res.Request.Name
	if name == "" {
		name = fmt.Sprintf("%s:%d", res.Request.File, res.Request.Line)
	}
	mark := "✓"
	if !res.OK {
		mark = "✗"
	}
	line := mark + " " + name
	if r := res.Response; r != nil {
		line += fmt.Sprintf(" · %d %s · %d ms", r.Status, r.StatusText, r.DurationMs)
	}
	if !res.OK {
		for _, a := range res.DisplayAsserts() {
			if !a.Pass {
				return line + " · " + a.Expr
			}
		}
		if len(res.Errors) > 0 {
			return line + " · " + res.Errors[0]
		}
	}
	return line
}

func describeSummary(req *httpfile.Request, d *runner.Description) string {
	var missing []string
	for _, v := range d.Variables {
		if v.Missing {
			missing = append(missing, v.Name)
		}
	}
	if d.Ready {
		return req.ID() + ": ready to send"
	}
	return req.ID() + ": missing " + strings.Join(missing, ", ")
}

// show puts a message in front of the user; log writes to the client's
// output for the server.
func (s *server) show(kind int, msg string) {
	_ = s.conn.notify("window/showMessage", showMessageParams{Type: kind, Message: msg})
}

func (s *server) log(msg string) {
	_ = s.conn.notify("window/logMessage", showMessageParams{Type: messageLog, Message: strings.TrimRight(msg, "\n")})
}
