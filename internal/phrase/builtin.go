package phrase

import (
	"fmt"
	"regexp"
	"strings"
)

// BuiltinStep is one of apic test's fixed vocabulary entries. The runner
// binds handlers by Name; the patterns live here so validation can detect
// `# @step` phrases that would be ambiguous with them.
type BuiltinStep struct {
	Name  string
	Regex string
}

// Builtin is the fixed step vocabulary, in registration order.
var Builtin = []BuiltinStep{
	{"environment", `^the environment is "([^"]*)"$`},
	{"variable", `^the variable "([^"]*)" is "([^"]*)"$`},
	{"variables", `^the variables:$`},
	{"run", `^I run "([^"]*)"$`},
	{"run-with", `^I run "([^"]*)" with:$`},
	{"run-file", `^I run the file "([^"]*)"$`},
	{"status", `^the response status is (\d+)$`},
	{"status-not", `^the response status is not (\d+)$`},
	{"status-class", `^the response is (successful|a client error|a server error)$`},
	{"compare", `^the response (body|header) "([^"]*)" (is not|is|equals|contains|starts with|ends with|matches) "([^"]*)"$`},
	{"exists", `^the response (body|header) "([^"]*)" (exists|does not exist)$`},
	{"body-equals", `^the response body is:$`},
	{"body-contains", `^the response body contains:$`},
	{"duration", `^the response time is under (\d+) ?ms$`},
	{"capture", `^I capture the response (body|header) "([^"]*)" as "([^"]*)"$`},
}

var builtinRegex = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(Builtin))
	for i, b := range Builtin {
		out[i] = regexp.MustCompile(b.Regex)
	}
	return out
}()

// Samples returns representative step texts for the phrase, with every
// parameter filled by a quoted word, a bare word and a number, used to
// probe for ambiguity with other patterns.
func (p *Phrase) Samples() []string {
	out := make([]string, 0, 3)
	for _, fill := range []string{`"sample"`, "sample", "123"} {
		out = append(out, reParam.ReplaceAllString(p.Text, fill))
	}
	return out
}

// ConflictsWithBuiltin reports whether text matching this phrase would also
// match a built-in step, which godog treats as ambiguous.
func (p *Phrase) ConflictsWithBuiltin() error {
	for _, sample := range p.Samples() {
		for i, re := range builtinRegex {
			if re.MatchString(sample) {
				return fmt.Errorf("@step %q is ambiguous with the built-in step %s (%s)", p.Text, Builtin[i].Name, strings.Trim(Builtin[i].Regex, "^$"))
			}
		}
	}
	return nil
}

// ConflictsWith reports whether either phrase's sample text matches the
// other's pattern.
func (p *Phrase) ConflictsWith(other *Phrase) bool {
	if p.Regex == other.Regex {
		return true
	}
	mine, theirs := regexp.MustCompile(p.Regex), regexp.MustCompile(other.Regex)
	for _, s := range p.Samples() {
		if theirs.MatchString(s) {
			return true
		}
	}
	for _, s := range other.Samples() {
		if mine.MatchString(s) {
			return true
		}
	}
	return false
}
