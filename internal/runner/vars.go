package runner

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"regexp"
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
	RefRuns    bool   `json:"ref_runs,omitempty"`    // CapturedBy is a `# @ref` of the request, so it runs first
}

// lookup resolves a plain variable name (no `$`, no response reference) for a
// request, walking the precedence layers.
func (r *Runner) lookup(req *httpfile.Request, name string, depth int) (VarInfo, bool, error) {
	info := VarInfo{Name: name}
	// --var and APIC_VAR_* are the documented way to inject a secret in CI
	// (docs/getting-started.md, docs/cookbook.md), and apic test already
	// treats them as secret. Mark them so here too, rather than leaving the
	// two halves of the product disagreeing about what a secret is.
	if v, ok := r.Opts.Vars[name]; ok {
		info.Value, info.Source, info.Secret = v, "--var", true
		return info, true, nil
	}
	if v, ok := os.LookupEnv("APIC_VAR_" + name); ok {
		info.Value, info.Source, info.Secret = v, "shell APIC_VAR_"+name, true
		return info, true, nil
	}
	if v, ok := r.captured[name]; ok {
		info.Value, info.Source, info.Secret = v, "captured this run", true
		return info, true, nil
	}
	if r.Session != nil {
		if v, ok := r.Session.Get(r.Opts.Env, name); ok {
			info.Value, info.Source, info.Secret = v, "session", true
			return info, true, nil
		}
	}
	if r.Envs != nil {
		if v, ok := r.Envs.PrivateVars(r.Opts.Env)[name]; ok {
			info.Value, info.Source, info.Secret = v, envSource("http-client.private.env.json", r.Opts.Env), true
			return info, true, nil
		}
		if v, ok := r.Envs.PublicVars(r.Opts.Env)[name]; ok {
			info.Value, info.Source = v, envSource("http-client.env.json", r.Opts.Env)
			return info, true, nil
		}
		if v, ok := r.Envs.DotEnv[name]; ok {
			info.Value, info.Source, info.Secret = v, ".env", true
			return info, true, nil
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
				return info, true, fmt.Errorf("variable %q exceeds max expansion depth", name)
			}
			secret := false
			v, err := template.Render(fv.Value, func(e string) (string, bool, error) {
				val, ok, sec, err := r.resolveExprMeta(req, e, depth+1)
				if sec {
					secret = true
				}
				return val, ok, err
			})
			if err != nil {
				return info, false, err
			}
			info.Value, info.Secret = v, secret
			return info, true, nil
		}
	}
	info.Missing = true
	info.Source = "missing"
	if req != nil {
		if by := r.Project.CapturedBy(name); by != nil && by != req {
			info.CapturedBy = by.ID()
		}
	}
	return info, false, nil
}

func envSource(file, env string) string {
	if env == "" {
		return file + " ($shared)"
	}
	return file + " [" + env + "]"
}

// resolveExpr resolves any `{{expr}}` for a request.
func (r *Runner) resolveExpr(req *httpfile.Request, expr string) (string, bool, error) {
	val, ok, _, err := r.resolveExprMeta(req, expr, 0)
	return val, ok, err
}

func (r *Runner) resolveExprMeta(req *httpfile.Request, expr string, depth int) (string, bool, bool, error) {
	if strings.HasPrefix(expr, "$auth.") {
		return r.authBuiltin(req, expr)
	}
	if strings.HasPrefix(expr, "$") {
		return r.builtin(expr)
	}
	if strings.Contains(expr, ".response.") {
		v, ok, err := r.responseRef(expr)
		return v, ok, false, err
	}
	info, ok, err := r.lookup(req, expr, depth)
	return info.Value, ok, info.Secret, err
}

// builtin resolves a `$name ...` placeholder: apic's own built-ins, REST
// Client's (with its offset grammar) and JetBrains' `$random.*` family.
func (r *Runner) builtin(expr string) (string, bool, bool, error) {
	if strings.HasPrefix(expr, "$random.") {
		v, err := randomBuiltin(expr)
		return v, err == nil, false, err
	}
	fields := strings.Fields(expr)
	name, args := fields[0], fields[1:]
	now := r.clock()
	switch name {
	case "$uuid", "$guid":
		return uuid.NewString(), true, false, nil
	case "$timestamp":
		t, err := offsetTime(name, now, args)
		if err != nil {
			return "", false, false, err
		}
		return strconv.FormatInt(t.Unix(), 10), true, false, nil
	case "$isoTimestamp":
		return now.UTC().Format(time.RFC3339), true, false, nil
	case "$datetime", "$localDatetime":
		layout, offset := datetimeArgs(strings.TrimSpace(strings.TrimPrefix(expr, name)))
		t, err := offsetTime(name, now, offset)
		if err != nil {
			return "", false, false, err
		}
		// $localDatetime keeps the clock's own zone (the machine's, or the
		// test's); $datetime is always UTC.
		if name == "$datetime" {
			t = t.UTC()
		}
		return t.Format(layout), true, false, nil
	case "$projectRoot":
		return r.Project.Root, true, false, nil
	case "$randomInt":
		lo, hi := 0, 1000
		var err error
		if len(args) >= 2 {
			if lo, err = strconv.Atoi(args[0]); err != nil {
				return "", false, false, fmt.Errorf("$randomInt min must be an integer")
			}
			if hi, err = strconv.Atoi(args[1]); err != nil {
				return "", false, false, fmt.Errorf("$randomInt max must be an integer")
			}
		}
		if hi <= lo {
			return "", false, false, fmt.Errorf("$randomInt max must be greater than min")
		}
		// {{$randomInt}} is a convenience for sample payloads, not a nonce or
		// a token; nothing in apic derives a credential from it.
		return strconv.Itoa(lo + rand.IntN(hi-lo)), true, false, nil //nolint:gosec // not used for anything security-sensitive
	case "$processEnv":
		if len(args) != 1 {
			return "", false, false, fmt.Errorf("$processEnv needs a variable name")
		}
		v, ok := os.LookupEnv(args[0])
		return v, ok, false, nil
	case "$dotenv":
		if len(args) != 1 {
			return "", false, false, fmt.Errorf("$dotenv needs a variable name")
		}
		if r.Envs == nil {
			return "", false, true, nil
		}
		v, ok := r.Envs.DotEnv[args[0]]
		return v, ok, true, nil
	}
	if strings.HasPrefix(name, "$env.") {
		v, ok := os.LookupEnv(strings.TrimPrefix(name, "$env."))
		return v, ok, false, nil
	}
	return "", false, false, fmt.Errorf("unknown built-in %s", name)
}

// clock is the time the built-ins read.
func (r *Runner) clock() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// offsetUnits are REST Client's offset units: `{{$timestamp -1 d}}`.
var offsetUnits = "ms, s, m, h, d, w, M, Q or y"

// offsetTime applies an optional `<n> <unit>` offset to t. Months, quarters
// and years move by calendar, the rest by duration.
func offsetTime(name string, t time.Time, args []string) (time.Time, error) {
	if len(args) == 0 {
		return t, nil
	}
	if len(args) != 2 {
		return t, fmt.Errorf("%s: an offset is `<n> <unit>` with the unit one of %s, got %q", name, offsetUnits, strings.Join(args, " "))
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return t, fmt.Errorf("%s: the offset %q is not an integer", name, args[0])
	}
	switch args[1] {
	case "ms":
		return t.Add(time.Duration(n) * time.Millisecond), nil
	case "s":
		return t.Add(time.Duration(n) * time.Second), nil
	case "m":
		return t.Add(time.Duration(n) * time.Minute), nil
	case "h":
		return t.Add(time.Duration(n) * time.Hour), nil
	case "d":
		return t.AddDate(0, 0, n), nil
	case "w":
		return t.AddDate(0, 0, 7*n), nil
	case "M":
		return t.AddDate(0, n, 0), nil
	case "Q":
		return t.AddDate(0, 3*n, 0), nil
	case "y":
		return t.AddDate(n, 0, 0), nil
	}
	return t, fmt.Errorf("%s: unknown offset unit %q; use one of %s", name, args[1], offsetUnits)
}

// datetimeArgs reads what follows `$datetime` or `$localDatetime`: an
// optional format (`rfc1123`, `iso8601` or a Go layout, quoted or not,
// spaces included) and an optional trailing `<n> <unit>` offset. No format
// means RFC 3339.
func datetimeArgs(rest string) (layout string, offset []string) {
	fields := strings.Fields(rest)
	if n := len(fields); n >= 2 && isOffsetUnit(fields[n-1]) {
		offset = fields[n-2:] // offsetTime checks the number
		fields = fields[:n-2]
	}
	switch f := strings.Trim(strings.Join(fields, " "), `"'`); f {
	case "", "iso8601":
		layout = time.RFC3339
	case "rfc1123":
		layout = time.RFC1123
	default:
		layout = f
	}
	return layout, offset
}

func isOffsetUnit(s string) bool {
	switch s {
	case "ms", "s", "m", "h", "d", "w", "M", "Q", "y":
		return true
	}
	return false
}

var reRandom = regexp.MustCompile(`^\$random\.([a-z]+)(?:\((.*)\))?$`)

// randomBuiltin resolves JetBrains' `$random.<kind>(args)`. The values are
// sample data for payloads, never credentials, so math/rand is the right
// source and the length defaults are small.
func randomBuiltin(expr string) (string, error) {
	m := reRandom.FindStringSubmatch(strings.TrimSpace(expr))
	if m == nil {
		return "", fmt.Errorf("%s: expected `$random.<kind>(args)`", expr)
	}
	kind := m[1]
	var args []string
	if strings.TrimSpace(m[2]) != "" {
		for _, a := range strings.Split(m[2], ",") {
			args = append(args, strings.TrimSpace(a))
		}
	}
	length := func(def int) (int, error) {
		if len(args) == 0 {
			return def, nil
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 0 || len(args) > 1 {
			return 0, fmt.Errorf("$random.%s takes one length, got (%s)", kind, m[2])
		}
		return n, nil
	}
	pick := func(alphabet string, n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = alphabet[rand.IntN(len(alphabet))] //nolint:gosec // sample data, not a secret
		}
		return string(b)
	}
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	switch kind {
	case "integer":
		lo, hi := int64(0), int64(1000)
		if len(args) != 0 {
			var err1, err2 error
			if len(args) != 2 {
				return "", fmt.Errorf("$random.integer takes (min, max), got (%s)", m[2])
			}
			lo, err1 = strconv.ParseInt(args[0], 10, 64)
			hi, err2 = strconv.ParseInt(args[1], 10, 64)
			if err1 != nil || err2 != nil || hi <= lo {
				return "", fmt.Errorf("$random.integer needs two integers with max greater than min, got (%s)", m[2])
			}
		}
		span := hi - lo
		if span <= 0 { // overflowed
			return "", fmt.Errorf("$random.integer: the range (%s) is too wide", m[2])
		}
		return strconv.FormatInt(lo+rand.Int64N(span), 10), nil //nolint:gosec // sample data, not a secret
	case "float":
		lo, hi := 0.0, 1000.0
		if len(args) != 0 {
			var err1, err2 error
			if len(args) != 2 {
				return "", fmt.Errorf("$random.float takes (min, max), got (%s)", m[2])
			}
			lo, err1 = strconv.ParseFloat(args[0], 64)
			hi, err2 = strconv.ParseFloat(args[1], 64)
			if err1 != nil || err2 != nil || math.IsNaN(lo) || math.IsNaN(hi) || math.IsInf(lo, 0) || math.IsInf(hi, 0) || hi <= lo {
				return "", fmt.Errorf("$random.float needs two finite numbers with max greater than min, got (%s)", m[2])
			}
		}
		return strconv.FormatFloat(lo+rand.Float64()*(hi-lo), 'f', 3, 64), nil //nolint:gosec // sample data, not a secret
	case "alphabetic":
		n, err := length(10)
		if err != nil {
			return "", err
		}
		return pick(letters, n), nil
	case "alphanumeric":
		n, err := length(10)
		if err != nil {
			return "", err
		}
		return pick(letters+"0123456789", n), nil
	case "hexadecimal":
		n, err := length(10)
		if err != nil {
			return "", err
		}
		return pick("0123456789abcdef", n), nil
	case "email":
		if len(args) != 0 {
			return "", fmt.Errorf("$random.email takes no arguments")
		}
		return strings.ToLower(pick(letters, 8)) + "@example.com", nil
	case "uuid":
		if len(args) != 0 {
			return "", fmt.Errorf("$random.uuid takes no arguments")
		}
		return uuid.NewString(), nil
	}
	return "", fmt.Errorf("unknown built-in $random.%s; the kinds are integer, float, alphabetic, alphanumeric, hexadecimal, email and uuid", kind)
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
