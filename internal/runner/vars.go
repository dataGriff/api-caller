package runner

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/selector"
	"github.com/dataGriff/api-caller/internal/template"
)

// VarInfo describes where a variable's value came from.
type VarInfo struct {
	Name       string `json:"name"`
	Value      string `json:"value,omitempty"`
	Source     string `json:"source"`
	Secret     bool   `json:"secret,omitempty"`
	Missing    bool   `json:"missing,omitempty"`
	CapturedBy string `json:"captured_by,omitempty"` // request that would provide it
}

// lookup resolves a plain variable name (no `$`, no response reference) for a
// request, walking the precedence layers.
func (r *Runner) lookup(req *httpfile.Request, name string, depth int) (VarInfo, bool) {
	info := VarInfo{Name: name}
	if v, ok := r.Opts.Vars[name]; ok {
		info.Value, info.Source = v, "--var"
		return info, true
	}
	if v, ok := os.LookupEnv("APIC_VAR_" + name); ok {
		info.Value, info.Source = v, "shell APIC_VAR_"+name
		return info, true
	}
	if v, ok := r.captured[name]; ok {
		info.Value, info.Source, info.Secret = v, "captured this run", true
		return info, true
	}
	if r.Session != nil {
		if v, ok := r.Session.Get(r.Opts.Env, name); ok {
			info.Value, info.Source, info.Secret = v, "session", true
			return info, true
		}
	}
	if r.Envs != nil {
		if v, ok := r.Envs.PrivateVars(r.Opts.Env)[name]; ok {
			info.Value, info.Source, info.Secret = v, envSource("http-client.private.env.json", r.Opts.Env), true
			return info, true
		}
		if v, ok := r.Envs.PublicVars(r.Opts.Env)[name]; ok {
			info.Value, info.Source = v, envSource("http-client.env.json", r.Opts.Env)
			return info, true
		}
		if v, ok := r.Envs.DotEnv[name]; ok {
			info.Value, info.Source, info.Secret = v, ".env", true
			return info, true
		}
	}
	if req != nil {
		// Last declaration wins, like REST Client.
		for i := len(req.File.Vars) - 1; i >= 0; i-- {
			fv := req.File.Vars[i]
			if fv.Name != name {
				continue
			}
			info.Source = fmt.Sprintf("%s:%d @%s", req.File.Path, fv.Line, fv.Name)
			if depth > 8 {
				info.Value = fv.Value
				return info, true
			}
			v, err := template.Render(fv.Value, func(e string) (string, bool, error) {
				return r.resolveExpr(req, e, depth+1)
			})
			if err != nil {
				info.Value = v
				return info, true
			}
			info.Value = v
			return info, true
		}
	}
	info.Missing = true
	info.Source = "missing"
	if req != nil {
		if by := r.Project.CapturedBy(name); by != nil && by != req {
			info.CapturedBy = by.ID()
		}
	}
	return info, false
}

func envSource(file, env string) string {
	if env == "" {
		return file + " ($shared)"
	}
	return file + " [" + env + "]"
}

// resolveExpr resolves any `{{expr}}` for a request.
func (r *Runner) resolveExpr(req *httpfile.Request, expr string, depth int) (string, bool, error) {
	if strings.HasPrefix(expr, "$") {
		return r.builtin(expr)
	}
	if strings.Contains(expr, ".response.") {
		return r.responseRef(expr)
	}
	info, ok := r.lookup(req, expr, depth)
	return info.Value, ok, nil
}

func (r *Runner) builtin(expr string) (string, bool, error) {
	fields := strings.Fields(expr)
	name, args := fields[0], fields[1:]
	switch name {
	case "$uuid", "$guid":
		return uuid.NewString(), true, nil
	case "$timestamp":
		return strconv.FormatInt(time.Now().Unix(), 10), true, nil
	case "$isoTimestamp":
		return time.Now().UTC().Format(time.RFC3339), true, nil
	case "$datetime":
		layout := time.RFC3339
		if len(args) > 0 {
			switch strings.Trim(args[0], `"'`) {
			case "rfc1123":
				layout = time.RFC1123
			case "iso8601":
				layout = time.RFC3339
			default:
				layout = strings.Trim(strings.Join(args, " "), `"'`)
			}
		}
		return time.Now().UTC().Format(layout), true, nil
	case "$randomInt":
		lo, hi := 0, 1000
		var err error
		if len(args) >= 2 {
			if lo, err = strconv.Atoi(args[0]); err != nil {
				return "", false, fmt.Errorf("$randomInt min must be an integer")
			}
			if hi, err = strconv.Atoi(args[1]); err != nil {
				return "", false, fmt.Errorf("$randomInt max must be an integer")
			}
		}
		if hi <= lo {
			return "", false, fmt.Errorf("$randomInt max must be greater than min")
		}
		return strconv.Itoa(lo + rand.IntN(hi-lo)), true, nil
	case "$processEnv":
		if len(args) != 1 {
			return "", false, fmt.Errorf("$processEnv needs a variable name")
		}
		v, ok := os.LookupEnv(args[0])
		return v, ok, nil
	case "$dotenv":
		if len(args) != 1 {
			return "", false, fmt.Errorf("$dotenv needs a variable name")
		}
		if r.Envs == nil {
			return "", false, nil
		}
		v, ok := r.Envs.DotEnv[args[0]]
		return v, ok, nil
	}
	if strings.HasPrefix(name, "$env.") {
		v, ok := os.LookupEnv(strings.TrimPrefix(name, "$env."))
		return v, ok, nil
	}
	return "", false, fmt.Errorf("unknown built-in %s", name)
}

// responseRef resolves `<name>.response.<selector>` against a request already
// run in this invocation, e.g. login.response.body.$.token.
func (r *Runner) responseRef(expr string) (string, bool, error) {
	name, sel, _ := strings.Cut(expr, ".response.")
	res, ok := r.results[name]
	if !ok || res.raw == nil {
		return "", false, fmt.Errorf("request %q has not been run in this invocation (run the whole file as a flow, or use @capture)", name)
	}
	v, found, err := selector.Select(res.raw, sel)
	if err != nil {
		return "", false, err
	}
	return v, found, nil
}
