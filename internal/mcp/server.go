// Package mcp exposes a project's requests to AI agents over the Model
// Context Protocol (stdio, or streamable HTTP behind a token), so an agent
// can discover and call an API without shelling out.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dataGriff/api-caller/internal/bdd"
	"github.com/dataGriff/api-caller/internal/curlexport"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
	"github.com/dataGriff/api-caller/internal/session"
)

// Config controls the server.
type Config struct {
	Dir     string // project root
	Env     string // default environment
	Version string
}

const instructions = `apic serves HTTP requests defined in plain .http files in a project.
Start with list_requests to see what is available, describe_request to learn
which variables a request needs, then run_request to send it. Values declared
with "# @capture" (for example a login token) are stored in the session and
reused by later calls automatically, so run a login request once and then
call the requests that depend on it. A request that declares "# @ref login"
runs login by itself when the token is missing; the result then lists what
ran first under ran_first. run_file runs every request in a file in
order as a flow. run_features runs the project's Gherkin .feature files and
reports which steps failed. validate_project checks every .http file without
sending anything, with the line and column of each problem, so run it after
editing a file. curl_request gives the equivalent curl command for a request.
Assertion failures come back as ok=false, not as errors.`

// New builds an MCP server for the project in cfg.Dir.
func New(cfg Config) (*sdk.Server, error) {
	root, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, err
	}
	s := &service{cfg: cfg, root: root}
	srv := sdk.NewServer(&sdk.Implementation{Name: "apic", Version: cfg.Version}, &sdk.ServerOptions{Instructions: instructions})

	sdk.AddTool(srv, &sdk.Tool{Name: "list_requests", Description: "List every request in the project with its id, method, URL template, file and description."}, s.listRequests)
	sdk.AddTool(srv, &sdk.Tool{Name: "describe_request", Description: "Show a request's variables and where each comes from, its captures and assertions, and whether it is ready to run."}, s.describeRequest)
	sdk.AddTool(srv, &sdk.Tool{Name: "run_request", Description: "Send one request and return status, headers, body, captures and assertion results. Captured values persist for later calls."}, s.runRequest)
	sdk.AddTool(srv, &sdk.Tool{Name: "run_file", Description: "Run every request in a .http file in order as a flow. Stops at the first failure unless keep_going is set."}, s.runFile)
	sdk.AddTool(srv, &sdk.Tool{Name: "list_environments", Description: "List the environments in http-client.env.json and the variables in effect (secrets masked)."}, s.listEnvironments)
	sdk.AddTool(srv, &sdk.Tool{Name: "clear_session", Description: "Forget captured values for an environment (or all of them)."}, s.clearSession)
	sdk.AddTool(srv, &sdk.Tool{Name: "run_features", Description: "Run Gherkin .feature files (default: features/ under the project) against the project's requests and return a pass/fail summary with the failing steps."}, s.runFeatures)
	sdk.AddTool(srv, &sdk.Tool{Name: "validate_project", Description: "Parse every .http file and report problems (bad selectors, unknown directives, duplicate names, missing body files, # @ref cycles) with file, line, column and a code, without sending any request. The same shape as `apic validate --json`."}, s.validateProject)
	sdk.AddTool(srv, &sdk.Tool{Name: "curl_request", Description: "The curl command equivalent to a request, with its variables resolved. Credentials are shell placeholders and header, body and query values are masked unless raw is set, so the command can go into a log or a ticket as it is."}, s.curlRequest)

	p, err := project.Load(root)
	if err != nil {
		return nil, err
	}
	// This advertised list is a snapshot, so a file added later is not listed
	// until the server restarts. Reads are re-validated against the project
	// as it is on disk (see resourcePath), so the two cannot drift into
	// serving something they should not.
	for _, f := range p.Files {
		abs := filepath.Join(root, filepath.FromSlash(f.Path))
		srv.AddResource(&sdk.Resource{URI: "file://" + filepath.ToSlash(abs), Name: f.Path, MIMEType: "text/plain",
			Description: fmt.Sprintf("%d request(s)", len(f.Requests))}, s.readFile)
	}
	return srv, nil
}

// Serve runs the server over stdio until the client disconnects.
func Serve(ctx context.Context, cfg Config) error {
	srv, err := New(cfg)
	if err != nil {
		return err
	}
	return srv.Run(ctx, &sdk.StdioTransport{})
}

type service struct {
	cfg  Config
	root string
}

func (s *service) newRunner(env string, vars map[string]string, keepGoing bool) (*runner.Runner, error) {
	p, err := project.Load(s.root)
	if err != nil {
		return nil, err
	}
	if env == "" {
		env = s.cfg.Env
	}
	runner.Version = s.cfg.Version
	r, err := runner.New(p, runner.Options{Env: env, Vars: vars, KeepGoing: keepGoing})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// toolError returns a tool-level error the agent can read and act on.
func toolError(err error) (*sdk.CallToolResult, any, error) {
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}}}, nil, nil
}

func structured(v any) (*sdk.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}, StructuredContent: json.RawMessage(b)}, nil, nil
}

type emptyInput struct{}

type requestSummary struct {
	ID          string   `json:"id"`
	Method      string   `json:"method"`
	URL         string   `json:"url"`
	File        string   `json:"file"`
	Line        int      `json:"line"`
	Description string   `json:"description,omitempty"`
	Captures    []string `json:"captures,omitempty"`
	Asserts     []string `json:"asserts,omitempty"`
	Refs        []string `json:"refs,omitempty"` // # @ref and # @forceRef targets
}

func (s *service) listRequests(_ context.Context, _ *sdk.CallToolRequest, _ emptyInput) (*sdk.CallToolResult, any, error) {
	p, err := project.Load(s.root)
	if err != nil {
		return toolError(err)
	}
	out := struct {
		Root     string           `json:"root"`
		Requests []requestSummary `json:"requests"`
	}{Root: p.Root, Requests: []requestSummary{}}
	for _, r := range p.Requests() {
		out.Requests = append(out.Requests, summarize(r))
	}
	return structured(out)
}

func summarize(r *httpfile.Request) requestSummary {
	e := requestSummary{ID: r.ID(), Method: r.Method, URL: r.URL, File: r.File.Path, Line: r.Line, Description: r.Description}
	for _, c := range r.Captures {
		e.Captures = append(e.Captures, c.Name)
	}
	for _, a := range r.Asserts {
		e.Asserts = append(e.Asserts, a.Expr)
	}
	for _, ref := range r.Refs() {
		e.Refs = append(e.Refs, ref.ID)
	}
	return e
}

func (s *service) validateProject(_ context.Context, _ *sdk.CallToolRequest, _ emptyInput) (*sdk.CallToolResult, any, error) {
	p, err := project.Load(s.root)
	if err != nil {
		return toolError(err)
	}
	diags := p.Validate()
	if diags == nil {
		diags = []httpfile.Diagnostic{}
	}
	ok := true
	for _, d := range diags {
		if d.Severity == "error" {
			ok = false
		}
	}
	return structured(struct {
		OK          bool                  `json:"ok"`
		Files       int                   `json:"files"`
		Requests    int                   `json:"requests"`
		Diagnostics []httpfile.Diagnostic `json:"diagnostics"`
	}{ok, len(p.Files), len(p.Requests()), diags})
}

type curlInput struct {
	Name string            `json:"name" jsonschema:"request id from list_requests"`
	Env  string            `json:"env,omitempty" jsonschema:"environment name; defaults to the server's --env"`
	Vars map[string]string `json:"vars,omitempty" jsonschema:"variable overrides, highest precedence"`
	Raw  bool              `json:"raw,omitempty" jsonschema:"include the live credentials and values, so the command runs as printed; by default they are placeholders and masks"`
}

func (s *service) curlRequest(_ context.Context, _ *sdk.CallToolRequest, in curlInput) (*sdk.CallToolResult, any, error) {
	r, err := s.newRunner(in.Env, in.Vars, false)
	if err != nil {
		return toolError(err)
	}
	req, err := single(r, in.Name)
	if err != nil {
		return toolError(err)
	}
	res, err := r.Resolve(req)
	if err != nil {
		return toolError(err)
	}
	if d := r.Describe(req); !d.Ready {
		var missing []string
		for _, v := range d.Variables {
			if v.Missing {
				missing = append(missing, v.Name)
			}
		}
		return toolError(r.MissingError(req, missing))
	}
	return structured(struct {
		ID      string `json:"id"`
		Command string `json:"command"`
	}{req.ID(), curlexport.Command(res, !in.Raw)})
}

type describeInput struct {
	Name string `json:"name" jsonschema:"request id from list_requests, e.g. get-user or users.http#2"`
	Env  string `json:"env,omitempty" jsonschema:"environment name; defaults to the server's --env"`
}

func (s *service) describeRequest(_ context.Context, _ *sdk.CallToolRequest, in describeInput) (*sdk.CallToolResult, any, error) {
	r, err := s.newRunner(in.Env, nil, false)
	if err != nil {
		return toolError(err)
	}
	req, err := single(r, in.Name)
	if err != nil {
		return toolError(err)
	}
	d := r.Describe(req)
	for i := range d.Variables {
		if d.Variables[i].Secret {
			d.Variables[i].Value = "***"
		}
	}
	return structured(d)
}

type runInput struct {
	Name string            `json:"name" jsonschema:"request id from list_requests"`
	Env  string            `json:"env,omitempty" jsonschema:"environment name; defaults to the server's --env"`
	Vars map[string]string `json:"vars,omitempty" jsonschema:"variable overrides, highest precedence"`
}

func (s *service) runRequest(ctx context.Context, _ *sdk.CallToolRequest, in runInput) (*sdk.CallToolResult, any, error) {
	r, err := s.newRunner(in.Env, in.Vars, false)
	if err != nil {
		return toolError(err)
	}
	req, err := single(r, in.Name)
	if err != nil {
		return toolError(err)
	}
	res, err := r.Run(ctx, req)
	if err != nil {
		return toolError(err)
	}
	return structured(res)
}

type runFileInput struct {
	File      string            `json:"file" jsonschema:"path of the .http file relative to the project root"`
	Env       string            `json:"env,omitempty"`
	Vars      map[string]string `json:"vars,omitempty"`
	KeepGoing bool              `json:"keep_going,omitempty" jsonschema:"continue after a failed request"`
}

func (s *service) runFile(ctx context.Context, _ *sdk.CallToolRequest, in runFileInput) (*sdk.CallToolResult, any, error) {
	r, err := s.newRunner(in.Env, in.Vars, in.KeepGoing)
	if err != nil {
		return toolError(err)
	}
	if !strings.Contains(in.File, ".http") && !strings.Contains(in.File, ".rest") {
		return toolError(fmt.Errorf("%q is not a .http file; use run_request for a single request", in.File))
	}
	reqs, err := r.Project.Resolve(in.File)
	if err != nil {
		return toolError(err)
	}
	results, runErr := r.RunAll(ctx, reqs)
	ok := runErr == nil
	for _, res := range results {
		if !res.OK {
			ok = false
		}
	}
	out := struct {
		OK      bool             `json:"ok"`
		Error   string           `json:"error,omitempty"`
		Results []*runner.Result `json:"results"`
	}{OK: ok, Results: results}
	if runErr != nil {
		out.Error = runErr.Error()
	}
	return structured(out)
}

type envInput struct {
	Env string `json:"env,omitempty"`
}

func (s *service) listEnvironments(_ context.Context, _ *sdk.CallToolRequest, in envInput) (*sdk.CallToolResult, any, error) {
	r, err := s.newRunner(in.Env, nil, false)
	if err != nil {
		return toolError(err)
	}
	vars := r.EnvVars()
	for i := range vars {
		if vars[i].Secret {
			vars[i].Value = "***"
		}
	}
	return structured(struct {
		Environments []string         `json:"environments"`
		Current      string           `json:"current,omitempty"`
		Files        []string         `json:"files"`
		Variables    []runner.VarInfo `json:"variables"`
	}{r.Envs.Names(), r.Opts.Env, r.Envs.Found, vars})
}

type clearInput struct {
	Env string `json:"env,omitempty"`
	All bool   `json:"all,omitempty" jsonschema:"clear every environment"`
}

func (s *service) clearSession(_ context.Context, _ *sdk.CallToolRequest, in clearInput) (*sdk.CallToolResult, any, error) {
	r, err := s.newRunner(in.Env, nil, false)
	if err != nil {
		return toolError(err)
	}
	target := r.Opts.Env
	if in.All {
		target = "*"
	}
	r.Session.Clear(target)
	if err := r.Session.Save(); err != nil {
		return toolError(err)
	}
	jar, err := session.OpenJar(r.Project.Root)
	if err != nil {
		return toolError(err)
	}
	jar.Clear(target)
	if err := jar.Save(); err != nil {
		return toolError(err)
	}
	return structured(map[string]any{"cleared": target})
}

type featuresInput struct {
	Paths      []string          `json:"paths,omitempty" jsonschema:"feature files or directories relative to the project root; default features/"`
	Tags       string            `json:"tags,omitempty" jsonschema:"tag expression such as @smoke && ~@slow"`
	Env        string            `json:"env,omitempty"`
	Vars       map[string]string `json:"vars,omitempty"`
	UseSession bool              `json:"use_session,omitempty" jsonschema:"share .apic/session.json with run_request and later calls (captures flow both ways); by default every scenario runs in an isolated in-memory session"`
}

func (s *service) runFeatures(ctx context.Context, _ *sdk.CallToolRequest, in featuresInput) (*sdk.CallToolResult, any, error) {
	p, err := project.Load(s.root)
	if err != nil {
		return toolError(err)
	}
	env := in.Env
	if env == "" {
		env = s.cfg.Env
	}
	if env == "" {
		env = p.Config.Env
	}
	runner.Version = s.cfg.Version
	sum, _, code, err := bdd.RunSummary(ctx, bdd.Options{
		Config: bdd.Config{Project: p, Env: env, Vars: in.Vars, UseSession: in.UseSession, Stderr: os.Stderr},
		Paths:  in.Paths, Tags: in.Tags,
	})
	if err != nil && sum == nil {
		return toolError(err)
	}
	out := struct {
		*bdd.Summary
		ExitCode int    `json:"exit_code"`
		Error    string `json:"error,omitempty"`
	}{Summary: sum, ExitCode: code}
	if err != nil {
		out.Error = err.Error()
		out.OK = false
	}
	return structured(out)
}

func (s *service) readFile(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
	real, err := s.resourcePath(req.Params.URI)
	if err != nil {
		return nil, err
	}
	// real is allowlisted against the project's own .http files by resourcePath.
	data, err := os.ReadFile(real) //nolint:gosec // allowlisted project file
	if err != nil {
		return nil, err
	}
	return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, MIMEType: "text/plain", Text: string(data)}}}, nil
}

// resourcePath resolves a resource URI to a file this server is willing to
// serve, or an error. Only the project's own .http files qualify: the private
// env file, .env and .apic/session.json all sit under the root and would
// otherwise be readable by any client that guessed the path.
//
// The allowlist is rebuilt per read rather than taken from the snapshot New
// registered, so a file added after the server started is readable, matching
// the tools, which re-load the project on every call.
func (s *service) resourcePath(uri string) (string, error) {
	path, ok := strings.CutPrefix(uri, "file://")
	if !ok {
		return "", fmt.Errorf("no such resource: %s", uri)
	}
	abs, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		return "", fmt.Errorf("resource outside project: %s", uri)
	}
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		return "", fmt.Errorf("resource outside project: %s", uri)
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("resource outside project: %s", uri)
	}
	p, err := project.Load(s.root)
	if err != nil {
		return "", err
	}
	for _, f := range p.Files {
		known, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(f.Path)))
		if err != nil {
			continue
		}
		if known == real {
			return real, nil
		}
	}
	// One message for "not a project file" and "does not exist", so the
	// server is not an existence oracle for files under the root.
	return "", fmt.Errorf("no such resource: %s", uri)
}

func single(r *runner.Runner, target string) (*httpfile.Request, error) {
	reqs, err := r.Project.Resolve(target)
	if err != nil {
		return nil, err
	}
	if len(reqs) != 1 {
		return nil, fmt.Errorf("%q names %d requests; use run_file for a whole file or %s#<name> for one", target, len(reqs), target)
	}
	return reqs[0], nil
}
