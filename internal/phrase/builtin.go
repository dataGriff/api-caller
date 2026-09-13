package phrase

import (
	"fmt"
	"strings"
)

// BuiltinStep is one of apic test's fixed vocabulary entries. The runner
// binds handlers by Name; the patterns live here so validation can detect
// `# @step` phrases that would be ambiguous with them. Shapes spell out the
// texts the regex accepts with every alternation expanded, so the overlap
// check can unify a phrase against them word by word. A capture is written
// the way the regex constrains it: "{x}" for a quoted value, {#} for a
// number, {#ms} for a number followed by ms, {x} for any single token.
type BuiltinStep struct {
	Name   string
	Regex  string
	Shapes []string
}

// Builtin is the fixed step vocabulary, in registration order.
var Builtin = []BuiltinStep{
	{"environment", `^the environment is "([^"]*)"$`, []string{`the environment is "{x}"`}},
	{"variable", `^the variable "([^"]+)" is "([^"]*)"$`, []string{`the variable "{x}" is "{y}"`}},
	{"variables", `^the variables:$`, []string{"the variables:"}},
	{"run", `^I run "([^"]*)"$`, []string{`I run "{x}"`}},
	{"run-with", `^I run "([^"]*)" with:$`, []string{`I run "{x}" with:`}},
	{"run-file", `^I run the file "([^"]*)"$`, []string{`I run the file "{x}"`}},
	{"status", `^the response status is (\d+)$`, []string{"the response status is {#}"}},
	{"status-not", `^the response status is not (\d+)$`, []string{"the response status is not {#}"}},
	{"status-class", `^the response is (successful|a client error|a server error)$`,
		[]string{"the response is successful", "the response is a client error", "the response is a server error"}},
	{"compare", `^the response (body|header) "([^"]*)" (is not|is|equals|contains|starts with|ends with|matches) "([^"]*)"$`,
		expand(`the response {where} "{sel}" {op} "{v}"`, map[string][]string{"{where}": {"body", "header"}, "{op}": {"is not", "is", "equals", "contains", "starts with", "ends with", "matches"}})},
	{"exists", `^the response (body|header) "([^"]*)" (exists|does not exist)$`,
		expand(`the response {where} "{sel}" {op}`, map[string][]string{"{where}": {"body", "header"}, "{op}": {"exists", "does not exist"}})},
	{"body-equals", `^the response body is:$`, []string{"the response body is:"}},
	{"body-contains", `^the response body contains:$`, []string{"the response body contains:"}},
	{"duration", `^the response time is under (\d+) ?ms$`, []string{"the response time is under {#} ms", "the response time is under {#ms}"}},
	{"capture", `^I capture the response (body|header) "([^"]*)" as "([^"]+)"$`,
		expand(`I capture the response {where} "{sel}" as "{name}"`, map[string][]string{"{where}": {"body", "header"}})},
}

// expand substitutes every combination of the given alternations into a shape.
func expand(shape string, alts map[string][]string) []string {
	out := []string{shape}
	for placeholder, words := range alts {
		var next []string
		for _, s := range out {
			for _, w := range words {
				next = append(next, strings.ReplaceAll(s, placeholder, w))
			}
		}
		out = next
	}
	return out
}

// MaxParams is the most parameters a phrase may declare.
const MaxParams = 6

// token is a word of a phrase: a literal, or a parameter. A parameter
// matches exactly one token of step text: a bare word, or a quoted value,
// which is a single token even when it contains spaces because no literal
// word can match text that includes quotes.
type token struct {
	literal string
	param   bool
	kind    paramKind
	// Literal text around a phrase placeholder inside one word, as in
	// `{id}foo`; a matching word must carry it.
	prefix, suffix string
}

// paramKind narrows what a parameter can match, mirroring the regex that
// backs it.
type paramKind int

const (
	anyToken      paramKind = iota // ("[^"]*"|\S+): a quoted value or a bare word
	quotedToken                    // "([^"]*)": quoted text only
	digitsToken                    // (\d+): digits only
	digitsMsToken                  // (\d+)ms: digits followed by ms, as one word
)

// tokenize splits step text into tokens. A double-quoted span is one token
// even when it contains spaces, matching how the step patterns treat it.
func tokenize(text string) []token {
	var out []token
	var cur strings.Builder
	inQuote, inWord := false, false
	flush := func() {
		if !inWord {
			return
		}
		w := cur.String()
		switch {
		case w == "{#}":
			out = append(out, token{param: true, kind: digitsToken})
		case w == "{#ms}":
			out = append(out, token{param: true, kind: digitsMsToken})
		case strings.Contains(w, "{") && isQuoted(w):
			out = append(out, token{param: true, kind: quotedToken})
		case strings.Contains(w, "{"):
			open, close := strings.Index(w, "{"), strings.LastIndex(w, "}")
			tok := token{param: true, kind: anyToken, prefix: w[:open]}
			if close >= 0 && close+1 <= len(w) {
				tok.suffix = w[close+1:]
			}
			out = append(out, tok)
		default:
			out = append(out, token{literal: w})
		}
		cur.Reset()
		inWord = false
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '"':
			inQuote = !inQuote
			cur.WriteByte(c)
			inWord = true
		case (c == ' ' || c == '\t') && !inQuote:
			flush()
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	flush()
	return out
}

func isQuoted(w string) bool {
	return len(w) >= 2 && w[0] == '"' && w[len(w)-1] == '"'
}

func isDigits(w string) bool {
	if w == "" {
		return false
	}
	for _, c := range w {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// overlap reports whether some step text could match both token patterns:
// the sequences must have the same length and, position by position, the
// tokens must admit a common word. Literals must be equal; a parameter
// admits what its kind allows, so a quoted-only capture never meets a bare
// word and a numeric capture never meets a quoted value.
func overlap(a, b []token) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !meet(a[i], b[i]) {
			return false
		}
	}
	return true
}

// meet reports whether one word can satisfy both tokens.
func meet(x, y token) bool {
	switch {
	case !x.param && !y.param:
		return x.literal == y.literal
	case x.param && y.param:
		if x.kind == anyToken && y.kind != anyToken {
			return affixesFit(x, y.kind)
		}
		if y.kind == anyToken && x.kind != anyToken {
			return affixesFit(y, x.kind)
		}
		if x.kind == anyToken && y.kind == anyToken {
			// Both free: a common word needs compatible affixes, e.g.
			// {x}foo and {y}bar can never match the same word.
			return (strings.HasPrefix(x.prefix, y.prefix) || strings.HasPrefix(y.prefix, x.prefix)) &&
				(strings.HasSuffix(x.suffix, y.suffix) || strings.HasSuffix(y.suffix, x.suffix))
		}
		return x.kind == y.kind
	case y.param:
		x, y = y, x
	}
	// x is the parameter, y the literal.
	switch x.kind {
	case quotedToken:
		return isQuoted(y.literal)
	case digitsToken:
		return isDigits(y.literal)
	case digitsMsToken:
		return strings.HasSuffix(y.literal, "ms") && isDigits(strings.TrimSuffix(y.literal, "ms"))
	}
	return strings.HasPrefix(y.literal, x.prefix) && strings.HasSuffix(y.literal, x.suffix) && len(y.literal) >= len(x.prefix)+len(x.suffix)
}

// affixesFit reports whether a free placeholder with literal affixes can
// still produce a word of the constrained kind.
func affixesFit(free token, kind paramKind) bool {
	switch kind {
	case quotedToken:
		return (free.prefix == "" || strings.HasPrefix(free.prefix, `"`)) && (free.suffix == "" || strings.HasSuffix(free.suffix, `"`))
	case digitsToken:
		return (free.prefix == "" || isDigits(free.prefix)) && (free.suffix == "" || isDigits(free.suffix))
	case digitsMsToken:
		okSuffix := free.suffix == "" || isDigits(free.suffix) || (strings.HasSuffix(free.suffix, "ms") && (free.suffix == "ms" || isDigits(strings.TrimSuffix(free.suffix, "ms"))))
		return (free.prefix == "" || isDigits(free.prefix)) && okSuffix
	}
	return true
}

// ConflictsWithBuiltin reports whether text matching this phrase could also
// match a built-in step, which godog treats as ambiguous.
func (p *Phrase) ConflictsWithBuiltin() error {
	mine := tokenize(p.Text)
	for _, b := range Builtin {
		for _, shape := range b.Shapes {
			if overlap(mine, tokenize(shape)) {
				return fmt.Errorf("@step %q could match the same text as the built-in step %s (%s)", p.Text, b.Name, strings.Trim(b.Regex, "^$"))
			}
		}
	}
	return nil
}

// ConflictsWith reports whether some step text could match both phrases.
func (p *Phrase) ConflictsWith(other *Phrase) bool {
	return overlap(tokenize(p.Text), tokenize(other.Text))
}
