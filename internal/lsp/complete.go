package lsp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dataGriff/api-caller/internal/history"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/runner"
)

// What to offer where in a request file: directives after `# @`,
// variables inside `{{`, selectors after `# @assert` and `# @capture x =`,
// operators, auth types and request names. The same table the VS Code
// extension's completion was built from, now in one place.

type contextKind int

const (
	ctxDirective contextKind = iota + 1
	ctxVariable
	ctxSelector
	ctxOperator
	ctxAuth
	ctxRef
)

// completionContext is what the cursor is completing: the text typed so
// far (prefix) and the byte column it starts at.
type completionContext struct {
	kind   contextKind
	prefix string
	start  int
	closed bool // a variable whose `}}` already follows the cursor
}

const comment = `^\s*(?:#|//)\s*`

var (
	directiveRe = regexp.MustCompile(comment + `@([\w-]*)$`)
	selectorRe  = regexp.MustCompile(comment + `@(assert\s+|capture\s+[\w.-]+\s*=\s*)(\S*)$`)
	operatorRe  = regexp.MustCompile(comment + `@assert\s+\S+\s+(\S*)$`)
	authRe      = regexp.MustCompile(comment + `@auth\s+(\S*)$`)
	refRe       = regexp.MustCompile(comment + `@(?:ref|forceRef)\s+(\S*)$`)
	variableRe  = regexp.MustCompile(`\{\{\s*([^{}]*)$`)
)

// contextAt reads the context at byte column col of a line.
func contextAt(line string, col int) *completionContext {
	before, after := line[:col], line[col:]
	if m := directiveRe.FindStringSubmatch(before); m != nil {
		return &completionContext{kind: ctxDirective, prefix: m[1], start: col - len(m[1]) - 1}
	}
	if m := variableRe.FindStringSubmatch(before); m != nil {
		return &completionContext{kind: ctxVariable, prefix: m[1], start: col - len(m[1]), closed: strings.HasPrefix(after, "}}")}
	}
	if m := selectorRe.FindStringSubmatch(before); m != nil {
		return &completionContext{kind: ctxSelector, prefix: m[2], start: col - len(m[2])}
	}
	if m := operatorRe.FindStringSubmatch(before); m != nil {
		return &completionContext{kind: ctxOperator, prefix: m[1], start: col - len(m[1])}
	}
	if m := authRe.FindStringSubmatch(before); m != nil {
		return &completionContext{kind: ctxAuth, prefix: m[1], start: col - len(m[1])}
	}
	if m := refRe.FindStringSubmatch(before); m != nil {
		return &completionContext{kind: ctxRef, prefix: m[1], start: col - len(m[1])}
	}
	return nil
}

// item is a suggestion before it is placed on the line.
type item struct {
	label, insert, detail, doc, sort string
	snippet                          bool
	kind                             int
}

// Operators are the assertion operators, in the order the docs list them.
var Operators = []string{
	"==", "!=", "<", "<=", ">", ">=", "contains", "startsWith", "endsWith", "matches", "exists", "not exists",
	"isString", "isNumber", "isInteger", "isBoolean", "isArray", "isObject", "isNull", "isEmpty", "not isEmpty",
	"length ==", "matchesSchema",
}

// authTypes are what `# @auth` takes, with the options each starts with.
var authTypes = [][2]string{
	{"bearer", "bearer {{${1:token}}}"},
	{"basic", "basic {{${1:user}}} {{${2:password}}}"},
	{"apikey", "apikey {{${1:apiKey}}} header=${2:X-Api-Key}"},
	{"digest", "digest {{${1:user}}} {{${2:password}}}"},
	{"aws", "aws region=${1:eu-west-2}"},
	{"oauth2", "oauth2 tokenUrl={{${1:tokenUrl}}} clientId={{${2:clientId}}} clientSecret={{${3:clientSecret}}}"},
	{"exec", "exec ${1:command}"},
	{"none", "none"},
}

// directiveInfo is a directive's snippet body and one-line description,
// from docs/format.md.
var directiveInfo = map[string][2]string{
	"name":        {"name ${1:request-name}", "Name used on the command line and by MCP."},
	"description": {"description ${1:text}", "One line shown by list and describe; defaults to the ### title."},
	"capture":     {"capture ${1:name} = ${2:body.$.}", "After the response arrives, store the selected value as a variable for later requests and the session."},
	"assert":      {"assert ${1:status} ${2|==,!=,<,<=,>,>=,contains,startsWith,endsWith,matches,exists,not exists|} ${3:200}", "Check the response. A failure sets ok: false and exit code 1."},
	"auth":        {"auth ${1|bearer,basic,apikey,digest,aws,oauth2,exec,none|} ", "Attach credentials: none, bearer, basic, apikey, digest, aws, oauth2 or exec."},
	"step":        {"step ${1:phrase}", "A Gherkin phrase that runs this request from a .feature file; {name} becomes a variable."},
	"ref":         {"ref ${1:login}", "Run the named request first when this one is missing a variable, once per invocation."},
	"forceRef":    {"forceRef ${1:login}", "Run the named request first every time this one runs."},
	"no-redirect": {"no-redirect", "Do not follow 3xx redirects."},
	"no-session":  {"no-session", "Do not persist this request's captures."},
	"no-cookies":  {"no-cookies", "Send no cookies with this request and keep none it sets."},
	"timeout":     {"timeout ${1:10s}", "Per-request timeout."},
	"retry":       {"retry ${1:5} ${2:1s}", "Re-send until every assertion passes, up to N times, this long apart."},
	"sleep":       {"sleep ${1:1s}", "Wait this long before sending, e.g. for a rate-limited API."},
	"disabled":    {"disabled", "Skip this request when its file runs as a flow; running it by name still sends it."},
	"note":        {"note ${1:text}", "Free text, accepted and ignored (REST Client compatibility)."},
	"prompt":      {"prompt ${1:name}", "Accepted and ignored: apic never prompts. Pass the value with --var or an env file."},
}

// directiveOrder is the order directives are offered in: the common ones
// first, as docs/format.md lists them.
var directiveOrder = []string{"name", "description", "capture", "assert", "auth", "ref", "forceRef", "step",
	"retry", "timeout", "sleep", "disabled", "no-redirect", "no-session", "no-cookies", "note", "prompt"}

// directiveNames is every directive the parser knows, in directiveOrder,
// then any newer one by name.
func directiveNames() []string {
	out := make([]string, 0, len(httpfile.KnownDirectives))
	seen := map[string]bool{}
	for _, n := range directiveOrder {
		if _, ok := httpfile.KnownDirectives[n]; ok {
			out = append(out, n)
			seen[n] = true
		}
	}
	var rest []string
	for n := range httpfile.KnownDirectives {
		if !seen[n] {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// builtin is a `{{$…}}` placeholder and what it produces.
type builtin struct{ name, insert, doc string }

var builtins = []builtin{
	{"$uuid", "$uuid", "random UUID v4"},
	{"$guid", "$guid", "random UUID v4"},
	{"$timestamp", "$timestamp", "Unix seconds"},
	{"$isoTimestamp", "$isoTimestamp", "RFC 3339 UTC"},
	{"$datetime", `$datetime ${1|rfc1123,iso8601,"2006-01-02"|}`, "formatted UTC time (Go layout for custom formats); an offset like `-1 d` may follow"},
	{"$localDatetime", `$localDatetime ${1|rfc1123,iso8601,"2006-01-02"|}`, "formatted time in the local zone; an offset like `1 h` may follow"},
	{"$randomInt", "$randomInt ${1:1} ${2:100}", "random integer in [min, max)"},
	{"$random.integer", "$random.integer(${1:1}, ${2:100})", "random integer in [min, max) (JetBrains)"},
	{"$random.float", "$random.float(${1:0}, ${2:1})", "random float with three decimals (JetBrains)"},
	{"$random.alphabetic", "$random.alphabetic(${1:10})", "random letters (JetBrains)"},
	{"$random.alphanumeric", "$random.alphanumeric(${1:10})", "random letters and digits (JetBrains)"},
	{"$random.hexadecimal", "$random.hexadecimal(${1:10})", "random hex digits (JetBrains)"},
	{"$random.email", "$random.email", "<8 letters>@example.com (JetBrains)"},
	{"$random.uuid", "$random.uuid", "random UUID v4 (JetBrains)"},
	{"$projectRoot", "$projectRoot", "the project root, absolute"},
	{"$processEnv", "$processEnv ${1:NAME}", "shell environment variable"},
	{"$env", "$env.${1:NAME}", "shell environment variable"},
	{"$dotenv", "$dotenv ${1:NAME}", "value from .env"},
	{"$auth.token", `$auth.token("${1:name}")`, "access token of a Security.Auth configuration in the env files (JetBrains)"},
	{"$auth.idToken", `$auth.idToken("${1:name}")`, "ID token of a Security.Auth configuration in the env files (JetBrains)"},
}

var selectors = []item{
	{label: "status", insert: "status", doc: "status code, e.g. 200"},
	{label: "statusText", insert: "statusText", doc: "e.g. OK"},
	{label: "header.", insert: "header.${1:content-type}", doc: "first value of a response header, case-insensitive", snippet: true},
	{label: "cookie.", insert: "cookie.${1:name}", doc: "value of a cookie the response set", snippet: true},
	{label: "body", insert: "body", doc: "raw body"},
	{label: "body.$", insert: "body.$", doc: "whole body (must be JSON)"},
	{label: "body.$.", insert: "body.$.${1:path}", doc: "JSON path such as body.$.items[0].id or body.$.items.# (count)", snippet: true},
	{label: "duration", insert: "duration", doc: "round-trip time in milliseconds"},
}

func (s *server) completion(p textDocumentPositionParams) (any, error) {
	path, ok := uriToPath(p.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	text := s.text(path)
	ls := lines(text)
	if p.Position.Line < 0 || p.Position.Line >= len(ls) {
		return nil, nil
	}
	line := ls[p.Position.Line]
	col := s.units.toByte(line, p.Position.Character)
	c := contextAt(line, col)
	if c == nil {
		return nil, nil
	}
	st := s.projectFor(path)
	var items []item
	switch c.kind {
	case ctxDirective:
		for i, name := range directiveNames() {
			body, doc := name+" ", httpfile.KnownDirectives[name]
			if info, ok := directiveInfo[name]; ok {
				body, doc = info[0], info[1]
			}
			items = append(items, item{label: "@" + name, insert: "@" + body, snippet: true, detail: "apic directive", doc: doc, sort: fmt.Sprintf("%02d", i), kind: kindKeyword})
		}
	case ctxVariable:
		items = s.variableItems(st, path, c.closed)
	case ctxSelector:
		items = s.selectorItems(st, path, text, p.Position.Line, c.prefix)
	case ctxOperator:
		for i, op := range Operators {
			items = append(items, item{label: op, insert: op, detail: "operator", sort: fmt.Sprintf("%02d", i), kind: kindOperator})
		}
	case ctxAuth:
		for i, a := range authTypes {
			items = append(items, item{label: a[0], insert: a[1], snippet: true, detail: "# @auth type", sort: fmt.Sprintf("%02d", i), kind: kindKeyword})
		}
	case ctxRef:
		if st.p != nil {
			seen := map[string]bool{}
			for _, r := range st.p.Requests() {
				if r.Name != "" && !seen[r.Name] {
					seen[r.Name] = true
					items = append(items, item{label: r.Name, insert: r.Name, detail: "request " + r.File.Path, kind: kindModule})
				}
			}
		}
	}
	edit := lspRange{
		Start: position{Line: p.Position.Line, Character: s.units.toClient(line, c.start)},
		End:   p.Position,
	}
	out := completionList{Items: make([]completionItem, 0, len(items))}
	for _, it := range items {
		ci := completionItem{Label: it.label, Kind: it.kind, Detail: it.detail, SortText: it.sort, FilterText: it.label,
			InsertTextFormat: formatPlain, TextEdit: &textEdit{Range: edit, NewText: it.insert}}
		if it.doc != "" {
			ci.Documentation = &markupContent{Kind: "markdown", Value: it.doc}
		}
		if it.snippet {
			if s.snippets {
				ci.InsertTextFormat = formatSnippet
				ci.TextEdit.NewText = escapeDollars(it.insert)
			} else {
				ci.TextEdit.NewText = plainSnippet(it.insert)
			}
		}
		out.Items = append(out.Items, ci)
	}
	return out, nil
}

// variableItems offers the variables (the source as detail, secrets
// masked: EnvVars includes the session's captures, masked as the secrets
// they may be), the built-ins and the response references of the file's
// named requests. Callers hold s.mu.
func (s *server) variableItems(st *state, path string, closed bool) []item {
	end := "}}"
	if closed {
		end = ""
	}
	var items []item
	seen := map[string]bool{}
	if r, err := s.runnerFor(st); err == nil {
		for _, v := range r.EnvVars() {
			if seen[v.Name] {
				continue
			}
			seen[v.Name] = true
			doc := "= " + v.Value
			switch {
			case v.Missing:
				doc = "not set"
			case v.Secret:
				doc = "= " + runner.Masked
			}
			items = append(items, item{label: v.Name, insert: v.Name + end, detail: v.Source, doc: doc, sort: "0" + v.Name, kind: kindVariable})
		}
	}
	for _, b := range builtins {
		items = append(items, item{label: b.name, insert: b.insert + end, snippet: true, detail: "built-in", doc: b.doc, sort: "1" + b.name, kind: kindKeyword})
	}
	if f := fileOf(st, path); f != nil {
		for _, r := range f.Requests {
			if r.Name == "" {
				continue
			}
			items = append(items,
				item{label: r.Name + ".response.body.$", insert: r.Name + ".response.body.$.${1:path}" + end, snippet: true, detail: "response reference", doc: "The JSON body " + r.Name + " received earlier in this run.", sort: "2" + r.Name, kind: kindField},
				item{label: r.Name + ".response.headers", insert: r.Name + ".response.headers.${1:name}" + end, snippet: true, detail: "response reference", doc: "A header " + r.Name + " received earlier in this run.", sort: "2" + r.Name, kind: kindField})
		}
	}
	return items
}

// selectorItems offers the selectors, or after `body.$` the keys of the
// last response the request got: from a run in this session, else the
// newest entry of its response history. Callers hold s.mu.
func (s *server) selectorItems(st *state, path, text string, line int, prefix string) []item {
	if strings.HasPrefix(prefix, "body.$") {
		if body, ok := s.lastBody(st, path, text, line); ok {
			if keys := bodyPathItems(prefix, body); len(keys) > 0 {
				return keys
			}
		}
	}
	items := make([]item, len(selectors))
	for i, sel := range selectors {
		sel.detail, sel.sort, sel.kind = "selector", fmt.Sprintf("%02d", i), kindField
		items[i] = sel
	}
	return items
}

func (s *server) lastBody(st *state, path, text string, line int) (any, bool) {
	if st.p == nil {
		return nil, false
	}
	req := requestAt(fileOf(st, path), text, line)
	if req == nil || req.Name == "" {
		return nil, false
	}
	r, err := s.runnerFor(st)
	if err != nil {
		return nil, false
	}
	// The environment in effect, apic.yaml's when nobody picked one: the
	// one runs record their history under.
	env := r.Opts.Env
	key := runner.HistoryKey(st.p, req)
	if b, ok := s.bodies[bodyKey(st.root, env, key)]; ok {
		return b, true
	}
	e, err := history.New(st.root, st.p.Config.History).Get(env, key, 1)
	if err != nil {
		return nil, false
	}
	res, err := runner.ParseResult(e.Result)
	if err != nil || res.Response == nil {
		return nil, false
	}
	return jsonBody(res.Response.Body)
}

// bodyKey is where a run's JSON body is kept for body.$ completion.
func bodyKey(root, env, key string) string { return root + "\x00" + env + "\x00" + key }

// jsonBody is a response body as a JSON object or array, whether it is
// still the raw JSON of a fresh result or already decoded from a stored
// one; ok is false for anything else.
func jsonBody(body any) (any, bool) {
	if raw, isRaw := body.(json.RawMessage); isRaw {
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, false
		}
	}
	switch body.(type) {
	case map[string]any, []any:
		return body, true
	}
	return nil, false
}

// bodyPathItems are the paths one level below what is typed, from a
// response body: an object's keys, or `#` and `[0]` for an array. prefix
// is the whole selector typed so far, `body.$.items.`.
func bodyPathItems(prefix string, body any) []item {
	typed := strings.TrimPrefix(prefix, "body.$")
	indexing := indexRe.MatchString(typed)
	rest := typed
	if indexing {
		rest = indexRe.ReplaceAllString(typed, "")
	}
	cut := max(strings.LastIndex(rest, "."), strings.LastIndex(rest, "["))
	p := ""
	switch {
	case indexing:
		p = rest
	case cut >= 0:
		p = rest[:cut+1]
	}
	node, ok := descend(body, p)
	if !ok {
		return nil
	}
	base := "body.$" + p
	sep := base
	if !strings.HasSuffix(sep, ".") {
		sep += "."
	}
	var items []item
	switch n := node.(type) {
	case []any:
		items = append(items, item{label: sep + "#", insert: sep + "#", detail: "count", doc: fmt.Sprintf("%d elements", len(n)), sort: "0", kind: kindField})
		if len(n) > 0 {
			at := strings.TrimSuffix(base, ".") + "[0]"
			items = append(items, item{label: at, insert: at, detail: describe(n[0]), sort: "1", kind: kindField})
		}
	case map[string]any:
		keys := make([]string, 0, len(n))
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			label := sep + k
			if !plainKeyRe.MatchString(k) {
				label = strings.TrimSuffix(sep, ".") + `["` + k + `"]`
			}
			items = append(items, item{label: label, insert: label, detail: describe(n[k]), sort: "2" + k, kind: kindField})
		}
	}
	return items
}

var (
	indexRe    = regexp.MustCompile(`\[\d*$`)
	plainKeyRe = regexp.MustCompile(`^[A-Za-z_][\w-]*$`)
)

var stepRe = regexp.MustCompile(`\.?([^.[\]]+)|\[(\d+)\]|\["([^"]*)"\]`)

// descend follows `.a.b[0].` through a JSON value.
func descend(root any, path string) (any, bool) {
	node := root
	for _, m := range stepRe.FindAllStringSubmatch(path, -1) {
		switch {
		case m[2] != "":
			arr, ok := node.([]any)
			i, _ := strconv.Atoi(m[2])
			if !ok || i >= len(arr) {
				return nil, false
			}
			node = arr[i]
		default:
			key := m[1]
			if m[3] != "" || strings.HasPrefix(m[0], `["`) {
				key = m[3]
			}
			if key == "" {
				continue
			}
			obj, ok := node.(map[string]any)
			if !ok {
				return nil, false
			}
			if node, ok = obj[key]; !ok {
				return nil, false
			}
		}
	}
	return node, true
}

func describe(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case []any:
		return fmt.Sprintf("array of %d", len(t))
	case map[string]any:
		return "object"
	}
	b, _ := json.Marshal(v)
	if s := string(b); len(s) > 40 {
		return s[:37] + "…"
	}
	return string(b)
}

var (
	choiceRe      = regexp.MustCompile(`\$\{\d+\|([^,|}]*)[^}]*\|\}`)
	placeholderRe = regexp.MustCompile(`\$\{\d+:([^}]*)\}`)
	tabstopRe     = regexp.MustCompile(`\$\d+`)
)

// escapeDollars makes every `$` that is apic's own (`$uuid`, `body.$.`)
// literal in a snippet, where `$name` would be a snippet variable: only a
// `$` before `{` or a digit is the snippet's.
func escapeDollars(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '$' && (i+1 >= len(s) || (s[i+1] != '{' && (s[i+1] < '0' || s[i+1] > '9'))) {
			b.WriteString(`\$`)
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// plainSnippet turns a snippet into the text a client without snippet
// support should insert: each placeholder's default, a choice's first
// option.
func plainSnippet(s string) string {
	s = choiceRe.ReplaceAllString(s, "$1")
	s = placeholderRe.ReplaceAllString(s, "$1")
	return tabstopRe.ReplaceAllString(s, "")
}
