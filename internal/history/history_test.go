package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func result(status int, body string) []byte {
	return []byte(`{"ok":true,"request":{"method":"GET","url":"http://x/"},"response":{"status":` +
		itoa(status) + `,"status_text":"OK","headers":{},"body":` + body + `,"duration_ms":12,"size":3}}`)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

var t0 = time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

func TestRecordKeepsTheNewestN(t *testing.T) {
	root := t.TempDir()
	s := New(root, 3)
	for i := range 5 {
		if err := s.Record("dev", "login", t0.Add(time.Duration(i)*time.Minute), result(200+i, `{"n":`+itoa(i)+`}`)); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.List("dev", "login")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("kept %d entries, want 3", len(list))
	}
	for i, e := range list {
		if e.Index != i+1 || e.Status != 204-i {
			t.Errorf("entry %d = index %d status %d, want index %d status %d", i, e.Index, e.Status, i+1, 204-i)
		}
		if e.Result != nil {
			t.Errorf("List returned a result for entry %d", e.Index)
		}
	}
	if list[0].DurationMs != 12 || list[0].Size != 3 || !list[0].OK || list[0].StatusText != "OK" {
		t.Errorf("summary = %+v", list[0])
	}
	if !list[0].Time.Equal(t0.Add(4 * time.Minute)) {
		t.Errorf("newest time = %v", list[0].Time)
	}
	if want := ".apic/history/dev/login/"; !strings.HasPrefix(list[0].File, want) {
		t.Errorf("file = %q, want it under %s", list[0].File, want)
	}
	e, err := s.Get("dev", "login", 3)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != 202 || !strings.Contains(compact(e.Result), `"n":2`) {
		t.Errorf("entry 3 = %d %s", e.Status, e.Result)
	}
	if _, err := os.Stat(filepath.Join(root, ".apic", ".gitignore")); err != nil {
		t.Errorf(".apic/.gitignore not written: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(e.File)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("entry mode = %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestEnvironmentsAndRequestsAreSeparate(t *testing.T) {
	s := New(t.TempDir(), 5)
	must(t, s.Record("", "login", t0, result(200, `1`)))
	must(t, s.Record("staging", "login", t0, result(500, `2`)))
	must(t, s.Record("", "whoami", t0, result(401, `3`)))
	for _, c := range []struct {
		env, name string
		status    int
	}{{"", "login", 200}, {"default", "login", 200}, {"staging", "login", 500}, {"", "whoami", 401}} {
		list, err := s.List(c.env, c.name)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].Status != c.status {
			t.Errorf("%q/%s = %+v, want one entry with %d", c.env, c.name, list, c.status)
		}
	}
	list, err := s.List("staging", "whoami")
	if err != nil || len(list) != 0 {
		t.Errorf("staging/whoami = %v, %v; want none", list, err)
	}
	must(t, s.Record("", "Get user/1", t0, result(200, `4`)))
	reqs, err := s.Requests("")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(reqs); got != "[{Get user/1 1} {login 1} {whoami 1}]" {
		t.Errorf("requests = %s", got)
	}
	if reqs, _ := s.Requests("nowhere"); len(reqs) != 0 {
		t.Errorf("an unknown environment has %v", reqs)
	}
}

func TestSameInstantEntriesKeepTheirOrder(t *testing.T) {
	s := New(t.TempDir(), 20)
	for i := range 12 {
		must(t, s.Record("", "poll", t0, result(200, itoa(i))))
	}
	e, err := s.Get("", "poll", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compact(e.Result), `"body":11`) {
		t.Errorf("newest = %s, want body 11", e.Result)
	}
	e, err = s.Get("", "poll", 12)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compact(e.Result), `"body":0,`) {
		t.Errorf("oldest = %s, want body 0", e.Result)
	}
}

func TestNamesCannotLeaveTheDirectory(t *testing.T) {
	root := t.TempDir()
	s := New(root, 2)
	for _, name := range []string{"../escape", "..", "a/b", `a\b`, "Get User"} {
		must(t, s.Record("../env", name, t0, result(200, `1`)))
		list, err := s.List("../env", name)
		if err != nil || len(list) != 1 {
			t.Fatalf("%q: %v, %v", name, list, err)
		}
		rel := filepath.FromSlash(list[0].File)
		if !strings.HasPrefix(rel, filepath.Join(".apic", "history")+string(filepath.Separator)) || strings.Count(filepath.ToSlash(rel), "/") != 4 {
			t.Errorf("%q stored at %s", name, list[0].File)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); err == nil {
		t.Error("a name climbed out of the history directory")
	}
}

func TestGetOutOfRange(t *testing.T) {
	s := New(t.TempDir(), 2)
	_, err := s.Get("", "login", 1)
	var re *RangeError
	if !errors.As(err, &re) || err.Error() != "no history for login" {
		t.Fatalf("err = %v", err)
	}
	must(t, s.Record("", "login", t0, result(200, `1`)))
	if _, err := s.Get("", "login", 2); err == nil || err.Error() != "login has 1 history entry, no #2" {
		t.Errorf("err = %v", err)
	}
}

func TestClear(t *testing.T) {
	s := New(t.TempDir(), 5)
	must(t, s.Record("", "login", t0, result(200, `1`)))
	must(t, s.Record("", "login", t0.Add(time.Second), result(200, `1`)))
	must(t, s.Record("", "whoami", t0, result(200, `1`)))
	must(t, s.Record("staging", "login", t0, result(200, `1`)))

	if n, err := s.Clear("", "login"); err != nil || n != 2 {
		t.Errorf("clear one request = %d, %v; want 2", n, err)
	}
	if list, _ := s.List("", "whoami"); len(list) != 1 {
		t.Error("clearing login cleared whoami")
	}
	if n, err := s.Clear("default", ""); err != nil || n != 1 {
		t.Errorf("clear the environment = %d, %v; want 1", n, err)
	}
	if list, _ := s.List("staging", "login"); len(list) != 1 {
		t.Error("clearing default cleared staging")
	}
	if n, err := s.Clear("*", ""); err != nil || n != 1 {
		t.Errorf("clear everything = %d, %v; want 1", n, err)
	}
	if n, err := s.Clear("*", ""); err != nil || n != 0 {
		t.Errorf("clear nothing = %d, %v", n, err)
	}
}

func TestKeepZeroRecordsNothing(t *testing.T) {
	root := t.TempDir()
	must(t, New(root, 0).Record("", "login", t0, result(200, `1`)))
	if _, err := os.Stat(filepath.Join(root, ".apic")); err == nil {
		t.Error("history 0 wrote .apic")
	}
}

func TestCompareJSON(t *testing.T) {
	from := result(200, `{"user":{"name":"alice","roles":["read"],"tags":{}},"token_type":"bearer","odd key":1,"n":1.50}`)
	to := result(201, `{"user":{"name":"bob","roles":["read","admin"],"tags":{}},"odd key":2,"n":1.50,"new":null}`)
	changes, err := Compare(from, to)
	if err != nil {
		t.Fatal(err)
	}
	got := render(changes)
	want := []string{
		"changed status 200 → 201",
		"added $.new  → null",
		"changed $['odd key'] 1 → 2",
		"removed $.token_type \"bearer\" → ",
		"changed $.user.name \"alice\" → \"bob\"",
		"added $.user.roles[1]  → \"admin\"",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("changes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if same, _ := Compare(from, from); len(same) != 0 {
		t.Errorf("identical results differ: %v", render(same))
	}
}

func TestCompareShapeChange(t *testing.T) {
	changes, err := Compare(result(200, `{"items":{"a":1}}`), result(200, `{"items":[1]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := render(changes); len(got) != 1 || got[0] != `changed $.items {"a":1} → [1]` {
		t.Errorf("changes = %q", got)
	}
}

func TestCompareText(t *testing.T) {
	changes, err := Compare(result(200, `"one\ntwo\nthree"`), result(200, `"one\n2\nthree\nfour"`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`removed line 2 "two" → `,
		`added line 2  → "2"`,
		`added line 4  → "four"`,
	}
	if got := render(changes); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("changes:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestCompareBinary(t *testing.T) {
	bin := func(b string) []byte {
		return []byte(`{"ok":true,"response":{"status":200,"body":"` + b + `","body_encoding":"base64"}}`)
	}
	changes, err := Compare(bin("AAE="), bin("AAI="))
	if err != nil {
		t.Fatal(err)
	}
	if got := render(changes); len(got) != 1 || got[0] != "changed body  → " {
		t.Errorf("changes = %q", got)
	}
	if same, _ := Compare(bin("AAE="), bin("AAE=")); len(same) != 0 {
		t.Errorf("same bytes differ: %q", render(same))
	}
}

func render(cs []Change) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Op+" "+c.Path+" "+string(c.From)+" → "+string(c.To))
	}
	return out
}

func compact(r json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, r); err != nil {
		return string(r)
	}
	return b.String()
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
