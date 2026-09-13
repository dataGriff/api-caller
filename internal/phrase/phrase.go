// Package phrase turns `# @step` phrases such as `a user named {name} exists`
// into anchored regular expressions whose capture groups map to variables.
package phrase

import (
	"fmt"
	"regexp"
	"strings"
)

// Parameter names follow the variable grammar (letters, digits, _ . -).
var reParam = regexp.MustCompile(`\{([A-Za-z_][\w.-]*)\}`)

// Phrase is a compiled step phrase.
type Phrase struct {
	Text   string   // as written
	Params []string // variable names in order of appearance
	Regex  string   // anchored pattern
}

// Parse compiles a phrase. Each {name} matches a double-quoted string or a
// bare word, so `a user named {name} exists` matches both
// `a user named "alice" exists` and `a user named alice exists`. A
// placeholder written as "{name}" matches quoted text only.
func Parse(text string) (*Phrase, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("@step needs a phrase")
	}
	if strings.ContainsAny(reParam.ReplaceAllString(text, ""), "{}") {
		return nil, fmt.Errorf("@step %q: braces must wrap a parameter name like {userId}", text)
	}
	p := &Phrase{Text: text}
	seen := map[string]bool{}
	var b strings.Builder
	b.WriteString("^")
	last := 0
	for _, m := range reParam.FindAllStringSubmatchIndex(text, -1) {
		b.WriteString(regexp.QuoteMeta(text[last:m[0]]))
		name := text[m[2]:m[3]]
		if seen[name] {
			return nil, fmt.Errorf("@step %q: parameter {%s} appears twice", text, name)
		}
		if strings.Contains(name, ".response.") {
			return nil, fmt.Errorf("@step %q: parameter {%s} uses \".response.\", which is reserved for response references", text, name)
		}
		seen[name] = true
		p.Params = append(p.Params, name)
		if m[0] > 0 && text[m[0]-1] == '"' && m[1] < len(text) && text[m[1]] == '"' {
			// `"{name}"`: the quotes are literal, the value is their content.
			b.WriteString(`([^"]*)`)
		} else {
			b.WriteString(`("[^"]*"|\S+)`)
		}
		last = m[1]
	}
	if len(p.Params) > MaxParams {
		return nil, fmt.Errorf("@step %q: more than %d parameters", text, MaxParams)
	}
	b.WriteString(regexp.QuoteMeta(text[last:]))
	b.WriteString("$")
	p.Regex = b.String()
	if _, err := regexp.Compile(p.Regex); err != nil {
		return nil, fmt.Errorf("@step %q: %v", text, err)
	}
	return p, nil
}

// Values pairs captured arguments (one per parameter, in order) with
// parameter names, stripping surrounding double quotes.
func (p *Phrase) Values(args []string) map[string]string {
	out := map[string]string{}
	for i, name := range p.Params {
		if i < len(args) {
			out[name] = Unquote(args[i])
		}
	}
	return out
}

// Unquote strips one pair of surrounding double quotes.
func Unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
