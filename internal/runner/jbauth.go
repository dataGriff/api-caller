package runner

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/env"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/template"
)

// reAuthBuiltin is JetBrains' {{$auth.token("name")}} and
// {{$auth.idToken("name")}}.
var reAuthBuiltin = regexp.MustCompile(`^\$auth\.(token|idToken)\(\s*"([^"]+)"\s*\)$`)

// authBuiltin resolves {{$auth.token("name")}} by running the grant of the
// env files' Security.Auth configuration "name", through the same token
// cache as `# @auth oauth2`. The value is a credential, so it is secret.
func (r *Runner) authBuiltin(req *httpfile.Request, expr string) (string, bool, bool, error) {
	m := reAuthBuiltin.FindStringSubmatch(expr)
	if m == nil {
		return "", false, true, Usagef(CodeAuth, `%s: write $auth.token("name") or $auth.idToken("name"), the name of a Security.Auth configuration in %s`, expr, env.PublicFile)
	}
	jb, err := r.jetBrainsAuth(req, m[2])
	if err != nil {
		return "", false, true, err
	}
	authEnv, err := r.authEnv()
	if err != nil {
		return "", false, true, err
	}
	wantID := m[1] == "idToken" || jb.UseIDToken
	// Built-ins resolve without a context; the token client has the
	// request timeout, and a device or browser sign-in its own.
	toks, err := auth.OAuth2Tokens(context.Background(), jb.Spec, authEnv, wantID)
	if err != nil {
		return "", false, true, Usagef(CodeAuth, "$auth %q: %v", m[2], err)
	}
	if wantID {
		return toks.ID, true, true, nil
	}
	return toks.Access, true, true, nil
}

// jetBrainsAuth maps the Security.Auth configuration name, for the
// current environment, with its placeholders resolved.
func (r *Runner) jetBrainsAuth(req *httpfile.Request, name string) (*auth.JetBrains, error) {
	var c *env.AuthConfig
	if r.Envs != nil {
		c = r.Envs.Auth(r.Opts.Env)[name]
	}
	if c == nil {
		return nil, Usagef(CodeAuth, "$auth %q: no Security.Auth configuration of that name for environment %q%s", name, r.Opts.Env, r.authNamesHint())
	}
	jb, err := auth.FromJetBrains(c.Fields, func(s string) (string, error) {
		return template.Render(s, func(e string) (string, bool, error) {
			if strings.HasPrefix(e, "$auth.") {
				return "", false, fmt.Errorf("$auth %q: a configuration cannot use $auth itself", name)
			}
			return r.resolveExpr(req, e)
		})
	})
	if err != nil {
		return nil, Usagef(CodeAuth, "$auth %q (Security.Auth in %s): %v", name, strings.Join(c.Files, ", "), err)
	}
	return jb, nil
}

func (r *Runner) authNamesHint() string {
	if r.Envs == nil {
		return ""
	}
	names := make([]string, 0)
	for n := range r.Envs.Auth(r.Opts.Env) {
		names = append(names, n)
	}
	if len(names) == 0 {
		return " (the env files declare none)"
	}
	sort.Strings(names)
	return " (declared: " + strings.Join(names, ", ") + ")"
}

// describeAuth is what describe says about an $auth placeholder: which
// configuration it runs, without fetching a token.
func (r *Runner) describeAuth(expr string) VarInfo {
	info := VarInfo{Name: expr, Secret: true}
	m := reAuthBuiltin.FindStringSubmatch(expr)
	if m == nil {
		_, _, _, err := r.authBuiltin(nil, expr)
		info.Source, info.Missing = err.Error(), true
		return info
	}
	cfg, ok := r.AuthConfig(m[2])
	switch {
	case !ok:
		info.Source, info.Missing = fmt.Sprintf("no Security.Auth configuration %q for environment %q%s", m[2], r.Opts.Env, r.authNamesHint()), true
	case cfg.Error != "":
		info.Source, info.Missing = fmt.Sprintf("Security.Auth %q: %s", m[2], cfg.Error), true
	default:
		info.Source = fmt.Sprintf("Security.Auth %q in %s: %s (fetched when sent)", m[2], strings.Join(cfg.Files, ", "), cfg.Spec)
	}
	return info
}

// AuthConfigInfo is one Security.Auth configuration, for apic env:
// secrets masked, placeholders as written.
type AuthConfigInfo struct {
	Name       string   `json:"name"`
	Spec       string   `json:"spec,omitempty"` // the oauth2 spec it maps to
	UseIDToken bool     `json:"use_id_token,omitempty"`
	Files      []string `json:"files"`
	Ignored    []string `json:"ignored,omitempty"` // fields apic does not act on
	Error      string   `json:"error,omitempty"`   // why it cannot be used
}

// AuthConfigs lists the Security.Auth configurations of the current
// environment, by name.
func (r *Runner) AuthConfigs() []AuthConfigInfo {
	if r.Envs == nil {
		return nil
	}
	var out []AuthConfigInfo
	for name := range r.Envs.Auth(r.Opts.Env) {
		info, _ := r.AuthConfig(name)
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AuthConfig describes one configuration without resolving anything.
func (r *Runner) AuthConfig(name string) (AuthConfigInfo, bool) {
	if r.Envs == nil {
		return AuthConfigInfo{}, false
	}
	c := r.Envs.Auth(r.Opts.Env)[name]
	if c == nil {
		return AuthConfigInfo{}, false
	}
	info := AuthConfigInfo{Name: name, Files: c.Files}
	jb, err := auth.FromJetBrains(c.Fields, func(s string) (string, error) { return s, nil })
	if err != nil {
		info.Error = err.Error()
		return info, true
	}
	info.Spec, info.UseIDToken, info.Ignored = jb.Spec.Raw, jb.UseIDToken, jb.Ignored
	return info, true
}
