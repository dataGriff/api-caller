// Package project discovers .http files under a directory and indexes their
// requests by name.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dataGriff/api-caller/internal/assert"
	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/phrase"
)

// ConfigFile is the optional per-project configuration file name.
const ConfigFile = "apic.yaml"

// Config is the content of apic.yaml.
type Config struct {
	Env     string     `yaml:"env"`     // default environment
	Dir     string     `yaml:"dir"`     // directory holding .http files, relative to the project root
	Timeout string     `yaml:"timeout"` // default request timeout, e.g. "30s"
	Auth    AuthConfig `yaml:"auth"`
	Test    TestConfig `yaml:"test"`
}

// TestConfig is the `test:` section of apic.yaml.
type TestConfig struct {
	Paths []string `yaml:"paths"` // feature files or directories, relative to the root (default: features)
}

// AuthConfig is the `auth:` section of apic.yaml.
type AuthConfig struct {
	Default   string `yaml:"default"`   // auth spec applied to requests without `# @auth`, e.g. "aws region=eu-west-2"
	AllowExec bool   `yaml:"allowExec"` // permit `# @auth exec ...`
}

// Project is a loaded set of .http files.
type Project struct {
	Root        string // absolute path of the project root (where apic.yaml / env files live)
	Config      Config
	Files       []*httpfile.File
	Diagnostics []httpfile.Diagnostic
	byName      map[string][]*httpfile.Request
	byPath      map[string]*httpfile.File
}

var skipDirs = map[string]bool{"node_modules": true, "vendor": true, ".git": true, ".apic": true, "testdata": true}

// Load discovers and parses every *.http and *.rest file under root.
func Load(root string) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	p := &Project{Root: abs, byName: map[string][]*httpfile.Request{}, byPath: map[string]*httpfile.File{}}
	if data, err := os.ReadFile(filepath.Join(abs, ConfigFile)); err == nil {
		if err := yaml.Unmarshal(data, &p.Config); err != nil {
			return nil, fmt.Errorf("%s: %w", ConfigFile, err)
		}
	}
	scan := abs
	if p.Config.Dir != "" {
		scan = filepath.Join(abs, p.Config.Dir)
	}
	var paths []string
	err = filepath.WalkDir(scan, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != scan && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(d.Name()); ext == ".http" || ext == ".rest" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	for _, path := range paths {
		f, diags, err := httpfile.ParseFile(path)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(abs, path)
		f.Path = filepath.ToSlash(rel)
		for i := range diags {
			diags[i].Path = f.Path
		}
		p.Diagnostics = append(p.Diagnostics, diags...)
		p.Files = append(p.Files, f)
		p.byPath[f.Path] = f
		for _, r := range f.Requests {
			if r.Name != "" {
				p.byName[r.Name] = append(p.byName[r.Name], r)
			}
		}
	}
	return p, nil
}

// Requests returns every request in file order.
func (p *Project) Requests() []*httpfile.Request {
	var out []*httpfile.Request
	for _, f := range p.Files {
		out = append(out, f.Requests...)
	}
	return out
}

// Lookup finds a request by name. It fails when the name is ambiguous.
func (p *Project) Lookup(name string) (*httpfile.Request, error) {
	rs := p.byName[name]
	switch len(rs) {
	case 0:
		return nil, p.notFound(name)
	case 1:
		return rs[0], nil
	}
	var where []string
	for _, r := range rs {
		where = append(where, fmt.Sprintf("%s:%d", r.File.Path, r.Line))
	}
	return nil, fmt.Errorf("request name %q is defined more than once (%s); use file#name", name, strings.Join(where, ", "))
}

// Resolve turns a command-line target into one or more requests:
//
//	get-user            a request by @name
//	users.http          every request in that file, in order
//	users.http#get-user a named request in a specific file
//	users.http#3        the third request in that file
func (p *Project) Resolve(target string) ([]*httpfile.Request, error) {
	file, frag, hasFrag := strings.Cut(target, "#")
	if !hasFrag {
		if f := p.fileFor(file); f != nil {
			return f.Requests, nil
		}
		r, err := p.Lookup(target)
		if err != nil {
			return nil, err
		}
		return []*httpfile.Request{r}, nil
	}
	f := p.fileFor(file)
	if f == nil {
		return nil, fmt.Errorf("no .http file %q in %s", file, p.Root)
	}
	if n, err := strconv.Atoi(frag); err == nil {
		if n < 1 || n > len(f.Requests) {
			return nil, fmt.Errorf("%s has %d requests, no #%d", f.Path, len(f.Requests), n)
		}
		return []*httpfile.Request{f.Requests[n-1]}, nil
	}
	for _, r := range f.Requests {
		if r.Name == frag {
			return []*httpfile.Request{r}, nil
		}
	}
	return nil, fmt.Errorf("no request named %q in %s", frag, f.Path)
}

func (p *Project) fileFor(name string) *httpfile.File {
	if !strings.HasSuffix(name, ".http") && !strings.HasSuffix(name, ".rest") {
		return nil
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if f, ok := p.byPath[clean]; ok {
		return f
	}
	if abs, err := filepath.Abs(name); err == nil {
		if rel, err := filepath.Rel(p.Root, abs); err == nil {
			if f, ok := p.byPath[filepath.ToSlash(rel)]; ok {
				return f
			}
		}
	}
	return nil
}

func (p *Project) notFound(name string) error {
	var names []string
	for n := range p.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("no request named %q: no named requests found under %s (add `# @name %s` above a request line)", name, p.Root, name)
	}
	if len(names) > 12 {
		names = append(names[:12], "...")
	}
	return fmt.Errorf("no request named %q; known: %s (run `apic list`)", name, strings.Join(names, ", "))
}

// Validate returns parse diagnostics plus project-level checks.
func (p *Project) Validate() []httpfile.Diagnostic {
	diags := append([]httpfile.Diagnostic(nil), p.Diagnostics...)
	for name, rs := range p.byName {
		if len(rs) > 1 {
			for _, r := range rs {
				diags = append(diags, httpfile.Diagnostic{Path: r.File.Path, Line: r.Line, Severity: "warning",
					Message: fmt.Sprintf("request name %q is also used elsewhere; `apic run %s` will be ambiguous", name, name)})
			}
		}
	}
	if p.Config.Auth.Default != "" {
		if spec, err := auth.Parse(p.Config.Auth.Default); err != nil {
			diags = append(diags, httpfile.Diagnostic{Path: ConfigFile, Line: 0, Severity: "error", Message: "auth.default: " + err.Error()})
		} else if spec.Type == "exec" && !p.Config.Auth.AllowExec {
			diags = append(diags, httpfile.Diagnostic{Path: ConfigFile, Line: 0, Severity: "warning", Message: "auth.default: @auth exec will be refused until apic.yaml sets auth.allowExec: true"})
		}
	}
	phrases := map[string]*httpfile.Request{}
	for _, r := range p.Requests() {
		for _, d := range r.Directives {
			if d.Key != "step" {
				continue
			}
			if _, err := phrase.Parse(d.Value); err != nil {
				diags = append(diags, httpfile.Diagnostic{Path: r.File.Path, Line: d.Line, Severity: "error", Message: err.Error()})
				continue
			}
			key := strings.TrimSpace(d.Value)
			if other, dup := phrases[key]; dup {
				diags = append(diags, httpfile.Diagnostic{Path: r.File.Path, Line: d.Line, Severity: "error",
					Message: fmt.Sprintf("@step %q is also declared on %s (%s:%d)", key, other.ID(), other.File.Path, other.Line)})
			}
			phrases[key] = r
		}
		for _, d := range r.Directives {
			if d.Key != "auth" {
				continue
			}
			spec, err := auth.Parse(d.Value)
			if err != nil {
				diags = append(diags, httpfile.Diagnostic{Path: r.File.Path, Line: d.Line, Severity: "error", Message: err.Error()})
			} else if spec.Type == "exec" && !p.Config.Auth.AllowExec {
				diags = append(diags, httpfile.Diagnostic{Path: r.File.Path, Line: d.Line, Severity: "warning", Message: "@auth exec will be refused until apic.yaml sets auth.allowExec: true"})
			}
		}
		for _, a := range r.Asserts {
			expr, err := assert.Parse(a.Expr)
			if err != nil {
				diags = append(diags, httpfile.Diagnostic{Path: r.File.Path, Line: a.Line, Severity: "error", Message: err.Error()})
				continue
			}
			if !validSelector(expr.Selector) {
				diags = append(diags, httpfile.Diagnostic{Path: r.File.Path, Line: a.Line, Severity: "error",
					Message: fmt.Sprintf("assert %q: unknown selector %q", a.Expr, expr.Selector)})
			}
		}
		for _, c := range r.Captures {
			if !validSelector(c.Selector) {
				diags = append(diags, httpfile.Diagnostic{Path: r.File.Path, Line: c.Line, Severity: "error",
					Message: fmt.Sprintf("capture %q: unknown selector %q", c.Name, c.Selector)})
			}
		}
		if r.BodyFile != "" {
			if _, err := os.Stat(filepath.Join(p.Root, filepath.Dir(r.File.Path), r.BodyFile)); errors.Is(err, fs.ErrNotExist) {
				diags = append(diags, httpfile.Diagnostic{Path: r.File.Path, Line: r.Line, Severity: "error",
					Message: fmt.Sprintf("body file %s not found", r.BodyFile)})
			}
		}
	}
	sort.SliceStable(diags, func(i, j int) bool {
		if diags[i].Path != diags[j].Path {
			return diags[i].Path < diags[j].Path
		}
		return diags[i].Line < diags[j].Line
	})
	return diags
}

func validSelector(s string) bool {
	switch {
	case s == "status", s == "statusText", s == "duration", s == "body", s == "body.$":
		return true
	case strings.HasPrefix(s, "header."), strings.HasPrefix(s, "headers."), strings.HasPrefix(s, "body.$."), strings.HasPrefix(s, "body.$["):
		return true
	}
	return false
}

// CapturedBy returns the first request that captures a variable of this name.
func (p *Project) CapturedBy(name string) *httpfile.Request {
	for _, r := range p.Requests() {
		for _, c := range r.Captures {
			if c.Name == name {
				return r
			}
		}
	}
	return nil
}
