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
	dashed, err := Parse("a user {user-id} with {a.b}")
	if err != nil || len(dashed.Params) != 2 || dashed.Params[0] != "user-id" || dashed.Params[1] != "a.b" {
		t.Fatalf("parameter names follow the variable grammar: %v %+v", err, dashed)
	}
	quoted, err := Parse(`I say "{value}" loudly`)
	if err != nil {
		t.Fatal(err)
	}
	qm := regexp.MustCompile(quoted.Regex).FindStringSubmatch(`I say "hello world" loudly`)
	if qm == nil || quoted.Values(qm[1:])["value"] != "hello world" {
		t.Fatalf("a quoted placeholder must match quoted text with spaces: %v", qm)
	}
	if regexp.MustCompile(quoted.Regex).MatchString(`I say hello loudly`) {
		t.Fatal("a quoted placeholder must not match a bare word")
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

func TestConflicts(t *testing.T) {
	if _, err := Parse("{a} {b} {c} {d} {e} {f} {g}"); err == nil {
		t.Error("seven parameters should be rejected")
	}
	for _, text := range []string{`I run {req}`, `the response status is {code}`, `I run "login"`, `the variables:`, `the response {what} "{sel}" is "{v}"`, `I capture the response {a} {b} as {c}`} {
		p, err := Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.ConflictsWithBuiltin(); err == nil {
			t.Errorf("%q should conflict with a built-in step", text)
		}
	}
	ok, _ := Parse("I log in as {user}")
	if err := ok.ConflictsWithBuiltin(); err != nil {
		t.Errorf("unexpected conflict: %v", err)
	}
	a, _ := Parse("a user named {name} exists")
	b, _ := Parse("a {role} named {name} exists")
	c, _ := Parse("a {role} called {name} exists")
	if !a.ConflictsWith(b) || !b.ConflictsWith(a) {
		t.Error("overlapping phrases should conflict")
	}
	if a.ConflictsWith(c) {
		t.Error("distinct phrases should not conflict")
	}
	// `I do foo` matches both of these, which fixed samples cannot detect.
	d, _ := Parse("I do {x}")
	e, _ := Parse("I {x} foo")
	if !d.ConflictsWith(e) {
		t.Error("`I do {x}` and `I {x} foo` both match `I do foo`")
	}
	f, _ := Parse("I log in as {user}")
	g, _ := Parse("I log out")
	if f.ConflictsWith(g) {
		t.Error("different literals must not conflict")
	}
	// A quoted span with spaces is one token, so this collides with `I run {x}`.
	q, err := Parse(`I run "foo bar"`)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.ConflictsWithBuiltin(); err == nil {
		t.Error(`I run "foo bar" should conflict with the built-in run step`)
	}
	h, _ := Parse(`I say "hello there" to {who}`)
	i, _ := Parse(`I say {what} to {who}`)
	if !h.ConflictsWith(i) {
		t.Error("quoted literal must unify with a parameter")
	}
}
