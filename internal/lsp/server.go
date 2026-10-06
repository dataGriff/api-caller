package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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
}

// Command names the server executes through workspace/executeCommand,
// offered by its code lenses. The arguments are the document URI and the
// request's target (file#name or file#N, relative to the project root).
const (
	CommandRun      = "apic.lsp.run"
	CommandDescribe = "apic.lsp.describe"
	CommandCurl     = "apic.lsp.curl"
)

// markers are the files whose directory is a project root.
var markers = []string{project.ConfigFile, env.PublicFile, env.PrivateFile}

// server is one client's session.
type server struct {
	conn *conn
	opts Options

	// mu guards everything below; a run started from a code lens finishes
	// on its own goroutine.
	mu          sync.Mutex
	initialized bool
	shutdown    bool
	roots       []string           // workspace folders, absolute
	env         string             // the environment in effect
	snippets    bool               // the client takes snippet completions
	watchable   bool               // the client lets the server register file watchers
	units       units              // how the client counts columns
	docs        map[string]string  // open documents by absolute path
	projects    map[string]*state  // by project root
	bodies      map[string]any     // last JSON body run here, by root + "\x00" + history key
	published   map[string]bool    // paths that have diagnostics showing
	wg          sync.WaitGroup     // runs in flight
	runCtx      context.Context    // cancelled on exit
	cancelRuns  context.CancelFunc //
}

// state is a loaded project and what the server derived from it.
type state struct {
	root string
	p    *project.Project
	err  error // why the project could not be loaded, shown as a diagnostic
}

// Serve runs a language server on r and w until the client sends exit or
// closes the stream. It returns nil after a shutdown request and an exit
// notification, as the protocol asks.
func Serve(ctx context.Context, r io.Reader, w io.Writer, opts Options) error {
	s := &server{conn: newConn(r, w), opts: opts, env: opts.Env, docs: map[string]string{},
		projects: map[string]*state{}, bodies: map[string]any{}, published: map[string]bool{}}
	s.runCtx, s.cancelRuns = context.WithCancel(ctx)
	defer func() {
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
	return errors.New("the client exited without a shutdown request")
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
					Env *string `json:"env"`
				} `json:"apic"`
			} `json:"settings"`
		}
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		if e := p.Settings.Apic.Env; e != nil {
			s.mu.Lock()
			s.env = *e
			s.mu.Unlock()
			s.refreshAll()
		}
		return nil, nil
	case "textDocument/completion":
		var p textDocumentPositionParams
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		return s.completion(p)
	case "textDocument/hover":
		var p textDocumentPositionParams
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
		return s.hover(p)
	case "textDocument/codeLens":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		if err := decode(m.Params, &p); err != nil {
			return nil, err
		}
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
	if e := p.InitializationOptions.Env; e != "" {
		s.env = e
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
	return map[string]any{
		"capabilities": map[string]any{
			"positionEncoding": encoding,
			"textDocumentSync": map[string]any{"openClose": true, "change": 1, "save": map[string]any{"includeText": false}},
			"completionProvider": map[string]any{
				"triggerCharacters": []string{"@", "{", ".", " ", "$"},
			},
			"hoverProvider":              true,
			"codeLensProvider":           map[string]any{"resolveProvider": false},
			"documentFormattingProvider": true,
			"executeCommandProvider":     map[string]any{"commands": []string{CommandRun, CommandDescribe, CommandCurl}},
		},
		"serverInfo": map[string]any{"name": "apic", "version": s.opts.Version},
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

// rootFor is the project a file belongs to: the nearest directory above it
// holding apic.yaml or an env file, without leaving its workspace folder;
// else that folder; else the file's own directory.
func (s *server) rootFor(path string) string {
	folder := ""
	for _, r := range s.roots {
		if rel, err := filepath.Rel(r, path); err == nil && !strings.HasPrefix(rel, "..") && len(r) > len(folder) {
			folder = r
		}
	}
	dir := filepath.Dir(path)
	for {
		for _, m := range markers {
			if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
				return dir
			}
		}
		if dir == folder {
			return folder
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if folder != "" {
		return folder
	}
	return filepath.Dir(path)
}

func (s *server) open(uri, text string) {
	path, ok := uriToPath(uri)
	if !ok {
		return
	}
	s.mu.Lock()
	s.docs[path] = text
	root := s.rootFor(path)
	s.mu.Unlock()
	s.refresh(root)
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
	s.refresh(root)
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
	s.refresh(root) // the file on disk is what counts now
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
		s.refresh(r)
	}
}

// refreshAll re-checks every project seen so far, after the environment
// changed.
func (s *server) refreshAll() {
	s.mu.Lock()
	var roots []string
	for r := range s.projects {
		roots = append(roots, r)
	}
	s.mu.Unlock()
	for _, r := range roots {
		s.refresh(r)
	}
}

// refresh reloads a project with the open buffers in place of their files
// and publishes its diagnostics, clearing those that went away.
func (s *server) refresh(root string) {
	s.mu.Lock()
	overlay := map[string]string{}
	for path, text := range s.docs {
		overlay[path] = text
	}
	st := &state{root: root}
	st.p, st.err = project.LoadOverlay(root, overlay)
	s.projects[root] = st
	diags := map[string][]diagnostic{}
	if st.err != nil {
		path := filepath.Join(root, project.ConfigFile)
		diags[path] = []diagnostic{{Range: lspRange{}, Severity: severityError, Source: "apic", Message: st.err.Error()}}
	} else {
		for _, d := range st.p.Validate() {
			path := filepath.Join(root, filepath.FromSlash(d.Path))
			diags[path] = append(diags[path], s.toDiagnostic(path, d))
		}
	}
	// Every open file of the project gets an answer, an empty one when it
	// is clean, so an editor never keeps markers from before it opened.
	for path := range s.docs {
		if _, has := diags[path]; !has && s.rootFor(path) == root {
			diags[path] = []diagnostic{}
		}
	}
	var clear []string
	for path := range s.published {
		if _, still := diags[path]; !still && s.rootFor(path) == root {
			clear = append(clear, path)
			delete(s.published, path)
		}
	}
	for path, ds := range diags {
		if len(ds) > 0 {
			s.published[path] = true
		}
	}
	s.mu.Unlock()

	paths := make([]string, 0, len(diags))
	for path := range diags {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		_ = s.conn.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: pathToURI(path), Diagnostics: diags[path]})
	}
	sort.Strings(clear)
	for _, path := range clear {
		_ = s.conn.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{URI: pathToURI(path), Diagnostics: []diagnostic{}})
	}
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
// 0-based line and character. A diagnostic without a column covers its
// whole line; one without a line, the start of the file.
func (s *server) toDiagnostic(path string, d httpfile.Diagnostic) diagnostic {
	ls := lines(s.text(path))
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

// project returns the loaded project a document belongs to, loading it
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

// runnerFor builds a runner over a loaded project for describing and
// completing, even when a file has errors: those are the diagnostics'
// business, and the rest of the project is still worth describing.
// Nothing it builds writes to disk. Callers hold s.mu.
func (s *server) runnerFor(st *state) (*runner.Runner, error) {
	if st.err != nil {
		return nil, st.err
	}
	p := *st.p
	p.Diagnostics = nil
	return runner.New(&p, runner.Options{Env: s.env, NoHistory: true})
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
	rel, err := filepath.Rel(st.root, path)
	if err != nil {
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
