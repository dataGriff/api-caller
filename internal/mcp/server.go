// Package mcp exposes a project's requests to AI agents over the Model
// Context Protocol (stdio), so an agent can discover and call an API without
// shelling out.
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
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/project"
	"github.com/dataGriff/api-caller/internal/runner"
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
call the requests that depend on it. run_file runs every request in a file in
order as a flow. run_features runs the project's Gherkin .feature files and
reports which steps failed. Assertion failures come back as ok=false, not as
errors.`

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

	p, err := project.Load(root)
	if err != nil {
		return nil, err
	}
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
	return e
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
	return structured(map[string]any{"cleared": target})
}

type featuresInput struct {
	Paths []string          `json:"paths,omitempty" jsonschema:"feature files or directories relative to the project root; default features/"`
	Tags  string            `json:"tags,omitempty" jsonschema:"tag expression such as @smoke && ~@slow"`
	Env   string            `json:"env,omitempty"`
	Vars  map[string]string `json:"vars,omitempty"`
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
	sum, _, _, err := bdd.RunSummary(ctx, bdd.Options{
		Config: bdd.Config{Project: p, Env: env, Vars: in.Vars, Stderr: os.Stderr},
		Paths:  in.Paths, Tags: in.Tags,
	})
	if err != nil {
		return toolError(err)
	}
	return structured(sum)
}

func (s *service) readFile(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
	path := strings.TrimPrefix(req.Params.URI, "file://")
	abs, err := filepath.Abs(filepath.FromSlash(path))
	if err != nil {
		return nil, fmt.Errorf("resource outside project: %s", req.Params.URI)
	}
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("resource outside project: %s", req.Params.URI)
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("resource outside project: %s", req.Params.URI)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return nil, err
	}
	return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, MIMEType: "text/plain", Text: string(data)}}}, nil
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
