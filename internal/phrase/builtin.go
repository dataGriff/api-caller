package phrase

import (
	"fmt"
	"strings"
)

// BuiltinStep is one of apic test's fixed vocabulary entries. The runner
// binds handlers by Name; the patterns live here so validation can detect
// `# @step` phrases that would be ambiguous with them. Shapes spell out the
// texts the regex accepts, with {x} standing for a single free token (a
// value capture) and every alternation expanded, so the overlap check can
// unify a phrase against them word by word.
type BuiltinStep struct {
	Name   string
	Regex  string
	Shapes []string
}

// Builtin is the fixed step vocabulary, in registration order.
var Builtin = []BuiltinStep{
	{"environment", `^the environment is "([^"]*)"$`, []string{"the environment is {x}"}},
	{"variable", `^the variable "([^"]*)" is "([^"]*)"$`, []string{"the variable {x} is {y}"}},
	{"variables", `^the variables:$`, []string{"the variables:"}},
	{"run", `^I run "([^"]*)"$`, []string{"I run {x}"}},
	{"run-with", `^I run "([^"]*)" with:$`, []string{"I run {x} with:"}},
	{"run-file", `^I run the file "([^"]*)"$`, []string{"I run the file {x}"}},
	{"status", `^the response status is (\d+)$`, []string{"the response status is {x}"}},
	{"status-not", `^the response status is not (\d+)$`, []string{"the response status is not {x}"}},
	{"status-class", `^the response is (successful|a client error|a server error)$`,
		[]string{"the response is successful", "the response is a client error", "the response is a server error"}},
	{"compare", `^the response (body|header) "([^"]*)" (is not|is|equals|contains|starts with|ends with|matches) "([^"]*)"$`,
		expand("the response {where} {sel} {op} {v}", map[string][]string{"{where}": {"body", "header"}, "{op}": {"is not", "is", "equals", "contains", "starts with", "ends with", "matches"}})},
	{"exists", `^the response (body|header) "([^"]*)" (exists|does not exist)$`,
		expand("the response {where} {sel} {op}", map[string][]string{"{where}": {"body", "header"}, "{op}": {"exists", "does not exist"}})},
	{"body-equals", `^the response body is:$`, []string{"the response body is:"}},
	{"body-contains", `^the response body contains:$`, []string{"the response body contains:"}},
	{"duration", `^the response time is under (\d+) ?ms$`, []string{"the response time is under {x} ms", "the response time is under {x}"}},
	{"capture", `^I capture the response (body|header) "([^"]*)" as "([^"]*)"$`,
		expand("I capture the response {where} {sel} as {name}", map[string][]string{"{where}": {"body", "header"}})},
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
}

func tokenize(text string) []token {
	var out []token
	for _, w := range strings.Fields(text) {
		if strings.Contains(w, "{") {
			out = append(out, token{param: true})
			continue
		}
		out = append(out, token{literal: w})
	}
	return out
}

// overlap reports whether some step text could match both token patterns:
// the sequences must have the same length and, position by position, a
// parameter matches anything while literals must be equal. This is exact
// for phrases (whose parameters are single tokens) and conservative for the
// built-in shapes, whose captures are narrower than a free token.
func overlap(a, b []token) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].param || b[i].param {
			continue
		}
		if a[i].literal != b[i].literal {
			return false
		}
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
