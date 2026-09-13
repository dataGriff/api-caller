package phrase

import (
	"regexp"
	"testing"
)

func TestParseAndMatch(t *testing.T) {
	p, err := Parse("a user named {name} with role {role} exists")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Params) != 2 || p.Params[0] != "name" || p.Params[1] != "role" {
		t.Fatalf("%+v", p)
	}
	re := regexp.MustCompile(p.Regex)
	for text, want := range map[string][2]string{
		`a user named "alice smith" with role admin exists`: {"alice smith", "admin"},
		`a user named bob with role "read only" exists`:     {"bob", "read only"},
		`a user named "" with role x exists`:                {"", "x"},
	} {
		m := re.FindStringSubmatch(text)
		if m == nil {
			t.Fatalf("no match: %s", text)
		}
		v := p.Values(m[1:])
		if v["name"] != want[0] || v["role"] != want[1] {
			t.Errorf("%s: %v", text, v)
		}
	}
	if re.MatchString("a user named alice exists") {
		t.Error("should not match with a missing parameter")
	}
	plain, _ := Parse("I fetch the user")
	if plain.Regex != "^I fetch the user$" || len(plain.Params) != 0 {
		t.Fatalf("%+v", plain)
	}
	for _, bad := range []string{"", "a {name} and {name}", "unbalanced {", "bad {1x}", "a user {name} }", "{a} {"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
