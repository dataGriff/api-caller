package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dataGriff/api-caller/internal/env"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
)

// Options configures a server.
type Options struct {
	// Root is the project root to use when the client names no workspace
	// (`apic lsp -C dir`).
	Root string
	// Env is the environment hover, completion and the run command use;
	// the client's initializationOptions.env replaces it.
	Env string
	// Version is reported to the client in serverInfo.
	Version string
	// Debounce is how long the server waits after an edit before it checks
	// the project again, so a burst of keystrokes costs one check. Zero
	// checks after every edit. A request (completion, hover, a lens) for a
	// project with a check pending runs the check first.
	Debounce time.Duration
}

// ErrNoShutdown is what Serve returns when the client exits, or closes
// the stream, without a shutdown request first; the protocol asks the
// server to exit 1 then.
var ErrNoShutdown = errors.New("the client exited without a shutdown request")

// Command names the server executes through workspace/executeCommand,
// offered by its code lenses. The arguments are the document URI and the
// request's target (file#name or file#N, relative to the project root).
const (
	CommandRun      = "apic.lsp.run"
	CommandDescribe = "apic.lsp.describe"
	CommandCurl     = "apic.lsp.curl"
	// CommandValidate checks every project of the workspace now and
	// answers once its diagnostics are published. It takes no arguments.
	CommandValidate = "apic.lsp.validate"
)

// markers are the files whose directory is a project root, as the VS
// Code extension looks for them.
var markers = []string{project.ConfigFile, env.PublicFile, env.PrivateFile}

// server is one client's session.
type server struct {
	conn *conn
	opts Options

	// mu guards everything below; a run started from a code lens and a
	// debounced check finish on their own goroutines.
	mu          sync.Mutex
	initialized bool
	shutdown    bool
	roots       []string                  // workspace folders, absolute
	projectDirs map[string]string         // workspace folder → the project root the client fixes for it
	env         string                    // the environment in effect where no project has its own
	envs        map[string]string         // the environment per project root, when the client picked one
	snippets    bool                      // the client takes snippet completions
	watchable   bool                      // the client lets the server register file watchers
	units       units                     // how the client counts columns
	docs        map[string]string         // open documents by absolute path
	projects    map[string]*state         // by project root
	runners     map[string]*runner.Runner // for describing and completing, by root + "\x00" + env
	pending     map[string]*time.Timer    // checks waiting out the debounce, by root
	bodies      map[string]any            // last JSON body run here, by root, env and history key
	published   map[string]string         // path → the root whose check published diagnostics for it
	sent        map[string][]diagnostic   // path → what was last published, so an unchanged set is not sent again
	wg          sync.WaitGroup            // runs and checks in flight
	runCtx      context.Context           // cancelled on exit
	cancelRuns  context.CancelFunc        //
}

// state is a loaded project and what the server derived from it.
type state struct {
	root string
	p    *project.Project
	err  error // why the project could not be loaded, shown as a diagnostic
}

// Serve runs a language server on r and w until the client sends exit or
// closes the stream. It returns nil after a shutdown request and an exit
// notification, and ErrNoShutdown when the client goes without one.
func Serve(ctx context.Context, r io.Reader, w io.Writer, opts Options) error {
	s := &server{conn: newConn(r, w), opts: opts, env: opts.Env, docs: map[string]string{},
		projectDirs: map[string]string{}, envs: map[string]string{}, projects: map[string]*state{},
		runners: map[string]*runner.Runner{}, pending: map[string]*time.Timer{}, bodies: map[string]any{},
		published: map[string]string{}, sent: map[string][]diagnostic{}}
	s.runCtx, s.cancelRuns = context.WithCancel(ctx)
	defer func() {
		s.mu.Lock()
		for root, t := range s.pending {
			if t.Stop() {
				s.wg.Done() // the check will not run, and owed this
			}
			delete(s.pending, root)
		}
		s.mu.Unlock()
		s.cancelRuns()
		s.wg.Wait()
	}()
	for {
		m, err := s.conn.read()
		if errors.Is(err, io.EOF) {
			return s.exitErr()
		}
		var re *rpcError
		if errors.As(err, &re) {
			_ = s.conn.reply(nil, nil, re)
			continue
		}
		if err != nil {
			return err
		}
		if m.Method == "" {
			continue // a reply to a request the server sent (registerCapability)
		}
		if m.Method == "exit" {
			return s.exitErr()
		}
		result, err := s.handle(m)
		if _, later := result.(deferred); later && err == nil {
			continue // a command that replies when it finishes
		}
		if m.ID != nil {
			if werr := s.conn.reply(m.ID, result, err); werr != nil {
				return werr
			}
		}
	}
}

func (s *server) exitErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shutdown {
		return nil
	}
	return ErrNoShutdown
}

// handle dispatches one request or notification.
func (s *server) handle(m *message) (any, error) {
	s.mu.Lock()
	ready, down := s.initialized, s.shutdown
	s.mu.Unlock()
	switch {
	case m.Method == "initialize":
		return s.initialize(m.Params)
	case !ready:
		if m.ID == nil {
			return nil, nil // notifications before initialize are dropped
		}
		return nil, &rpcError{Code: codeNotInitialized, Message: "the server is not initialized"}
	case down && m.ID != nil:
		return nil, &rpcError{Code: codeInvalidRequest, Message: "the server is shutting down"}
	}
	switch m.Method {
	case "initialized":
		s.registerWatchers()
		s.validateWorkspace()
		return nil, nil
	case "shutdown":
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		return nil, nil
	case "textDocument/didOpen":
		var p didOpenParams
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		s.open(p.TextDocument.URI, p.TextDocument.Text)
		return nil, nil
	case "textDocument/didChange":
		var p didChangeParams
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		s.change(p)
		return nil, nil
	case "textDocument/didSave":
		return nil, nil // the buffer already holds what was saved
	case "textDocument/didClose":
		var p didCloseParams
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		s.close(p.TextDocument.URI)
		return nil, nil
	case "workspace/didChangeWatchedFiles":
		var p didChangeWatchedFilesParams
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		s.watched(p)
		return nil, nil
	case "workspace/didChangeConfiguration":
		var p struct {
			Settings struct {
				Apic struct {
					Env  *string           `json:"env"`
					Envs map[string]string `json:"envs"`
				} `json:"apic"`
			} `json:"settings"`
		}
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		s.mu.Lock()
		if e := p.Settings.Apic.Env; e != nil {
			s.env = *e
		}
		s.setEnvs(p.Settings.Apic.Envs)
		s.runners = map[string]*runner.Runner{}
		s.mu.Unlock()
		return nil, nil
	case "textDocument/completion":
		var p textDocumentPositionParams
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		s.settle(p.TextDocument.URI)
		return s.completion(p)
	case "textDocument/hover":
		var p textDocumentPositionParams
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		s.settle(p.TextDocument.URI)
		return s.hover(p)
	case "textDocument/codeLens":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		s.settle(p.TextDocument.URI)
		return s.codeLenses(p.TextDocument.URI), nil
	case "textDocument/formatting":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		return s.format(p.TextDocument.URI), nil
	case "workspace/executeCommand":
		var p executeCommandParams
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		return s.execute(m.ID, p)
	}
	if m.ID == nil || strings.HasPrefix(m.Method, "$/") {
		return nil, nil // unknown notifications, and $/ requests, are ignored
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "apic does not handle " + m.Method}
}

func decode(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	return nil
}

// setEnvs records the environments the client picked per project root; an
// empty value goes back to the default. Callers hold s.mu.
func (s *server) setEnvs(envs map[string]string) {
	for root, e := range envs {
		root = filepath.Clean(root)
		if e == "" {
			delete(s.envs, root)
		} else {
			s.envs[root] = e
		}
	}
}

// envFor is the environment a project runs in: the one the client picked
// for it, else the server's, else (when that is empty too) apic.yaml's,
// which runner.New applies. Callers hold s.mu.
func (s *server) envFor(root string) string {
	if e, ok := s.envs[root]; ok {
		return e
	}
	return s.env
}

func (s *server) initialize(raw json.RawMessage) (any, error) {
	var p initializeParams
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range p.WorkspaceFolders {
		if path, ok := uriToPath(f.URI); ok {
			s.roots = append(s.roots, path)
		}
	}
	if len(s.roots) == 0 {
		if path, ok := uriToPath(p.RootURI); ok {
			s.roots = []string{path}
		} else if p.RootPath != "" {
			s.roots = []string{filepath.Clean(p.RootPath)}
		} else if s.opts.Root != "" {
			if abs, err := filepath.Abs(s.opts.Root); err == nil {
				s.roots = []string{abs}
			}
		}
	}
	o := p.InitializationOptions
	if o.Env != "" {
		s.env = o.Env
	}
	s.setEnvs(o.Envs)
	for folder, root := range o.ProjectRoots {
		s.projectDirs[filepath.Clean(folder)] = filepath.Clean(root)
	}
	s.snippets = p.Capabilities.TextDocument.Completion.CompletionItem.SnippetSupport
	encoding := "utf-16"
	for _, e := range p.Capabilities.General.PositionEncodings {
		if e == "utf-8" {
			encoding = "utf-8"
			s.units = units{utf8: true}
		}
	}
	s.initialized = true
	s.watchable = p.Capabilities.Workspace.DidChangeWatchedFiles.DynamicRegistration
	caps := map[string]any{
		"positionEncoding": encoding,
		"textDocumentSync": map[string]any{"openClose": true, "change": 1, "save": map[string]any{"includeText": false}},
		"completionProvider": map[string]any{
			"triggerCharacters": []string{"@", "{", ".", " ", "$"},
		},
		"hoverProvider":          true,
		"executeCommandProvider": map[string]any{"commands": []string{CommandRun, CommandDescribe, CommandCurl, CommandValidate}},
	}
	if o.CodeLens == nil || *o.CodeLens {
		caps["codeLensProvider"] = map[string]any{"resolveProvider": false}
	}
	if o.Formatting == nil || *o.Formatting {
		caps["documentFormattingProvider"] = true
	}
	return map[string]any{
		"capabilities": caps,
		"serverInfo":   map[string]any{"name": "apic", "version": s.opts.Version},
	}, nil
}

// registerWatchers asks a client that can to tell the server about
// changes to request files, apic.yaml and the env files made outside the
// editor (a git checkout, another tool).
func (s *server) registerWatchers() {
	s.mu.Lock()
	ok := s.watchable
	s.mu.Unlock()
	if !ok {
		return
	}
	id := json.RawMessage(`"apic-watch"`)
	params, _ := json.Marshal(map[string]any{"registrations": []map[string]any{{
		"id": "apic-watch", "method": "workspace/didChangeWatchedFiles",
		"registerOptions": map[string]any{"watchers": []map[string]any{
			{"globPattern": "**/*.{http,rest}"},
			{"globPattern": "**/{apic.yaml,http-client.env.json,http-client.private.env.json,.env}"},
		}},
	}}})
	_ = s.conn.write(&message{ID: &id, Method: "client/registerCapability", Params: params})
}

// rootFor is the project a file belongs to: inside a workspace folder,
// the root the client fixed for that folder, else the nearest directory
// above the file holding apic.yaml or an http-client env file without
// leaving the folder, else the folder; outside every folder, the file's
// own directory. Callers hold s.mu.
func (s *server) rootFor(path string) string {
	folder := ""
	for _, r := range s.roots {
		if _, in := project.Within(r, path); in && len(r) > len(folder) {
			folder = r
		}
	}
	if folder == "" {
		return filepath.Dir(path)
	}
	if fixed, ok := s.projectDirs[folder]; ok {
		return fixed
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		for _, m := range markers {
			if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
				return dir
			}
		}
		if dir == folder || filepath.Dir(dir) == dir {
			return folder
		}
	}
}

// rootMemo is rootFor with its answers kept, for a check that asks about
// many files. Callers hold s.mu.
func (s *server) rootMemo() func(string) string {
	seen := map[string]string{}
	return func(path string) string {
		if r, ok := seen[path]; ok {
			return r
		}
		r := s.rootFor(path)
		seen[path] = r
		return r
	}
}

func (s *server) open(uri, text string) {
	path, ok := uriToPath(uri)
	if !ok {
		return
	}
	s.mu.Lock()
	s.docs[path] = text
	delete(s.sent, path) // a client may have dropped them when the file closed
	root := s.rootFor(path)
	s.mu.Unlock()
	s.refresh(root) // a newly opened file gets its diagnostics at once
}

func (s *server) change(p didChangeParams) {
	path, ok := uriToPath(p.TextDocument.URI)
	if !ok || len(p.ContentChanges) == 0 {
		return
	}
	s.mu.Lock()
	// The server asks for full sync, so the last change is the whole text.
	s.docs[path] = p.ContentChanges[len(p.ContentChanges)-1].Text
	root := s.rootFor(path)
	s.mu.Unlock()
	s.schedule(root)
}

func (s *server) close(uri string) {
	path, ok := uriToPath(uri)
	if !ok {
		return
	}
	s.mu.Lock()
	delete(s.docs, path)
	root := s.rootFor(path)
	s.mu.Unlock()
	s.schedule(root) // the file on disk is what counts now
}

// watched re-checks the projects a change outside the editor touched.
func (s *server) watched(p didChangeWatchedFilesParams) {
	roots := map[string]bool{}
	s.mu.Lock()
	for _, c := range p.Changes {
		if path, ok := uriToPath(c.URI); ok {
			roots[s.rootFor(path)] = true
		}
	}
	s.mu.Unlock()
	for r := range roots {
		s.schedule(r)
	}
}

// schedule checks a project once the edits stop for Options.Debounce,
// or at once without a debounce.
func (s *server) schedule(root string) {
	if s.opts.Debounce <= 0 {
		s.refresh(root)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.pending[root]; ok && t.Stop() {
		s.wg.Done() // replaced before it ran
	}
	s.wg.Add(1)
	var t *time.Timer
	t = time.AfterFunc(s.opts.Debounce, func() {
		defer s.wg.Done()
		s.mu.Lock()
		current := s.pending[root] == t
		if current {
			delete(s.pending, root)
		}
		s.mu.Unlock()
		if current {
			s.refresh(root)
		}
	})
	s.pending[root] = t
}

// settle runs a pending check of a document's project now, so a request
// about the document sees its latest text.
func (s *server) settle(uri string) {
	path, ok := uriToPath(uri)
	if !ok {
		return
	}
	s.mu.Lock()
	root := s.rootFor(path)
	t, pending := s.pending[root]
	if pending {
		delete(s.pending, root)
	}
	s.mu.Unlock()
	if pending {
		if t.Stop() {
			s.wg.Done() // the timer will not run its function, which owed this
		}
		s.refresh(root)
	}
}

// validateWorkspace checks the project of every workspace folder, every
// project nested inside one, and every project a document has opened
// since, publishing diagnostics for files that are not open too, as
// `apic validate` in each project would report them.
func (s *server) validateWorkspace() {
	s.mu.Lock()
	var roots []string
	for _, folder := range s.roots {
		// rootFor walks up from a file; a name inside the folder starts
		// the walk at the folder itself.
		roots = append(roots, s.rootFor(filepath.Join(folder, "_")))
	}
	for r := range s.projects {
		roots = append(roots, r)
	}
	s.mu.Unlock()
	done := map[string]bool{}
	for len(roots) > 0 {
		r := roots[0]
		roots = roots[1:]
		if done[r] {
			continue
		}
		done[r] = true
		roots = append(roots, s.refresh(r)...)
	}
}

// refresh reloads a project with the open buffers in place of their files
// and publishes the diagnostics of the files it owns, clearing those that
// went away. A file under the root that belongs to a project nested
// inside it is that project's business: refresh returns those roots.
func (s *server) refresh(root string) []string {
	s.mu.Lock()
	overlay := map[string]string{}
	for path, text := range s.docs {
		overlay[path] = text
	}
	st := &state{root: root}
	st.p, st.err = project.LoadOverlay(root, overlay)
	s.projects[root] = st
	for k := range s.runners {
		if strings.HasPrefix(k, root+"\x00") {
			delete(s.runners, k)
		}
	}
	owner := s.rootMemo()
	nested := map[string]bool{}
	diags := map[string][]diagnostic{}
	if st.err != nil {
		path := filepath.Join(root, project.ConfigFile)
		diags[path] = []diagnostic{{Range: lspRange{}, Severity: severityError, Source: "apic", Message: st.err.Error()}}
	} else {
		for _, f := range st.p.Files {
			path := filepath.Join(root, filepath.FromSlash(f.Path))
			if o := owner(path); o != root {
				nested[o] = true
			}
		}
		texts := map[string][]string{}
		for _, d := range st.p.Validate() {
			path := filepath.Join(root, filepath.FromSlash(d.Path))
			if owner(path) != root && filepath.Dir(path) != root {
				continue // a nested project's file: its own check publishes it
			}
			ls, ok := texts[path]
			if !ok {
				ls = lines(s.text(path))
				texts[path] = ls
			}
			diags[path] = append(diags[path], s.toDiagnostic(ls, d))
		}
	}
	// Every open file of the project gets an answer, an empty one when it
	// is clean, so an editor never keeps markers from before it opened.
	for path := range s.docs {
		if _, has := diags[path]; !has && owner(path) == root {
			diags[path] = []diagnostic{}
		}
	}
	for path, by := range s.published {
		if _, still := diags[path]; !still && by == root {
			diags[path] = []diagnostic{}
		}
	}
	var send []string
	for path, ds := range diags {
		if len(ds) > 0 {
			s.published[path] = root
		} else {
			delete(s.published, path)
		}
		if prev, ok := s.sent[path]; ok && reflect.DeepEqual(prev, ds) {
			continue
		}
		s.sent[path] = ds
		send = append(send, path)
	}
	s.mu.Unlock()

	sort.Strings(send)
	for _, path := range send {
		_ = s.conn.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: pathToURI(path), Diagnostics: diags[path]})
	}
	var more []string
	for r := range nested {
		more = append(more, r)
	}
	sort.Strings(more)
	return more
}

// text is a file's content: the open buffer, else what is on disk.
// Callers hold s.mu.
func (s *server) text(path string) string {
	if t, ok := s.docs[path]; ok {
		return t
	}
	data, err := os.ReadFile(path) //nolint:gosec // a file of the project the client opened
	if err != nil {
		return ""
	}
	return string(data)
}

// toDiagnostic converts apic's 1-based byte columns to the client's
// 0-based line and character, given the file's lines. A diagnostic
// without a column covers its whole line; one without a line, the start
// of the file.
func (s *server) toDiagnostic(ls []string, d httpfile.Diagnostic) diagnostic {
	lineText := func(n int) string {
		if n >= 0 && n < len(ls) {
			return ls[n]
		}
		return ""
	}
	out := diagnostic{Severity: severityError, Source: "apic", Code: d.Code, Message: d.Message}
	if d.Severity == "warning" {
		out.Severity = severityWarning
	}
	if d.Code != "" {
		out.CodeDescription = &codeDescription{Href: "https://datagriff.github.io/api-caller/cli/#apic-validate"}
	}
	if d.Line <= 0 {
		return out
	}
	start := d.Line - 1
	text := lineText(start)
	if d.Column <= 0 {
		out.Range = lspRange{Start: position{Line: start}, End: position{Line: start, Character: s.units.toClient(text, len(text))}}
		return out
	}
	end, endCol := start, d.EndColumn
	if d.EndLine > 0 {
		end = d.EndLine - 1
	}
	if endCol <= 0 {
		endCol = len(lineText(end)) + 1
	}
	out.Range = lspRange{
		Start: position{Line: start, Character: s.units.toClient(text, d.Column-1)},
		End:   position{Line: end, Character: s.units.toClient(lineText(end), endCol-1)},
	}
	return out
}

// projectFor returns the loaded project a document belongs to, loading it
// the first time. Callers hold s.mu.
func (s *server) projectFor(path string) *state {
	root := s.rootFor(path)
	if st, ok := s.projects[root]; ok {
		return st
	}
	overlay := map[string]string{}
	for p, text := range s.docs {
		overlay[p] = text
	}
	st := &state{root: root}
	st.p, st.err = project.LoadOverlay(root, overlay)
	s.projects[root] = st
	return st
}

// runnerFor is a runner over a loaded project for describing and
// completing, even when a file has errors: those are the diagnostics'
// business, and the rest of the project is still worth describing.
// Nothing it does writes to disk. It is kept until the project is checked
// again or the environment changes. Callers hold s.mu.
func (s *server) runnerFor(st *state) (*runner.Runner, error) {
	if st.err != nil {
		return nil, st.err
	}
	env := s.envFor(st.root)
	key := st.root + "\x00" + env
	if r, ok := s.runners[key]; ok {
		return r, nil
	}
	p := *st.p
	p.Diagnostics = nil
	r, err := runner.New(&p, runner.Options{Env: env, NoHistory: true})
	if err != nil {
		return nil, err
	}
	s.runners[key] = r
	return r, nil
}

// requestAt is the request whose block holds 0-based line n of a file:
// the blocks are split by ### lines, as the parser splits them.
func requestAt(f *httpfile.File, text string, n int) *httpfile.Request {
	if f == nil {
		return nil
	}
	ls := lines(text)
	start, end := 0, len(ls)-1
	for i := min(n, len(ls)-1); i >= 0; i-- {
		if strings.HasPrefix(ls[i], "###") {
			start = i
			break
		}
	}
	for i := n + 1; i < len(ls); i++ {
		if strings.HasPrefix(ls[i], "###") {
			end = i - 1
			break
		}
	}
	for _, r := range f.Requests {
		if r.Line-1 >= start && r.Line-1 <= end {
			return r
		}
	}
	return nil
}

// fileOf returns the parsed file at path in a project.
func fileOf(st *state, path string) *httpfile.File {
	if st == nil || st.p == nil {
		return nil
	}
	rel, ok := project.Within(st.root, path)
	if !ok {
		return nil
	}
	return st.p.File(filepath.ToSlash(rel))
}

// target is a request's run target relative to its project: file#name,
// or file#N for an unnamed one.
func target(r *httpfile.Request) string {
	if r.Name != "" {
		return r.File.Path + "#" + r.Name
	}
	return fmt.Sprintf("%s#%d", r.File.Path, r.Index)
}
