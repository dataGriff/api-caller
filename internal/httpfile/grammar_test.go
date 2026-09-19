package httpfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestGrammarMatchesKnownDirectives keeps the VS Code extension's
// highlighting in step with the parser: the directives its grammar marks
// as known must be exactly KnownDirectives, so the editor never shows a
// directive as valid that `apic validate` reports as unknown, or the
// reverse.
func TestGrammarMatchesKnownDirectives(t *testing.T) {
	path := filepath.Join("..", "..", "editors", "vscode", "syntaxes", "apic-directives.injection.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no extension grammar at %s: %v", path, err)
	}
	var grammar struct {
		Patterns []struct {
			Name  string `json:"name"`
			Match string `json:"match"`
		} `json:"patterns"`
	}
	if err := json.Unmarshal(data, &grammar); err != nil {
		t.Fatal(err)
	}
	var match string
	for _, p := range grammar.Patterns {
		if p.Name == "meta.directive.apic" {
			match = p.Match
		}
	}
	if match == "" {
		t.Fatal("grammar has no meta.directive.apic pattern")
	}
	// The pattern lists the directives as `(@(?:a|b|c))`.
	m := regexp.MustCompile(`\(@\(\?:([^)]*)\)\)`).FindStringSubmatch(match)
	if m == nil {
		t.Fatalf("cannot find the directive alternation in %q", match)
	}
	got := strings.Split(m[1], "|")
	sort.Strings(got)
	want := make([]string, 0, len(KnownDirectives))
	for k := range KnownDirectives {
		want = append(want, k)
	}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the extension grammar lists %v\nbut the parser knows %v\nkeep %s in step with KnownDirectives", got, want, path)
	}
}
