package session

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestJarRecordsWhatTheServerSetsAndReplaysItAfterReopen(t *testing.T) {
	root := t.TempDir()
	j, err := OpenJar(root)
	if err != nil {
		t.Fatal(err)
	}
	// net/http/cookiejar keeps its own clock, so lifetimes are relative to
	// real time; the jar's clock is pinned for the expiry bookkeeping.
	now := time.Now()
	j.now = func() time.Time { return now }
	e := j.HTTP("dev")
	login := mustURL("https://api.example.com/auth/login")
	e.SetCookies(login, []*http.Cookie{
		{Name: "sid", Value: "abc"}, // host-only, default path /auth, session cookie
		{Name: "pref", Value: "dark", Domain: "example.com", Path: "/", MaxAge: 3600}, // domain cookie with a lifetime
		{Name: "old", Value: "x", Expires: now.Add(-time.Hour)},                       // already expired: dropped
		{Name: "other", Value: "y", Domain: "evil.com"},                               // rejected by the jar: not recorded
		{Name: "gone", Value: "z", MaxAge: -1},                                        // deletion
	})
	got := j.Cookies("dev")
	if len(got) != 2 {
		t.Fatalf("cookies = %+v", got)
	}
	if got[0] != (Cookie{Name: "sid", Value: "abc", Domain: "api.example.com", Path: "/auth", HostOnly: true}) {
		t.Errorf("host-only cookie = %+v", got[0])
	}
	if got[1] != (Cookie{Name: "pref", Value: "dark", Domain: "example.com", Path: "/", Expires: now.Add(time.Hour)}) {
		t.Errorf("domain cookie = %+v", got[1])
	}
	// Sent where they apply, and not elsewhere.
	names := func(u string) []string {
		var out []string
		for _, c := range e.Cookies(mustURL(u)) {
			out = append(out, c.Name)
		}
		return out
	}
	if n := names("https://api.example.com/auth/me"); len(n) != 2 {
		t.Errorf("same host and path: %v", n)
	}
	if n := names("https://api.example.com/todos"); len(n) != 1 || n[0] != "pref" {
		t.Errorf("other path: %v", n)
	}
	if n := names("https://www.example.com/"); len(n) != 1 || n[0] != "pref" {
		t.Errorf("sibling host gets the domain cookie only: %v", n)
	}
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	// File modes are not meaningful on Windows.
	if info, err := os.Stat(filepath.Join(root, Dir, CookieFile)); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Errorf("cookie file: %v %v", info, err)
	}

	// A new process sees the same cookies, minus the ones that expired.
	j2, err := OpenJar(root)
	if err != nil {
		t.Fatal(err)
	}
	j2.now = func() time.Time { return now.Add(30 * time.Minute) }
	e2 := j2.HTTP("dev")
	if n := len(e2.Cookies(mustURL("https://api.example.com/auth/me"))); n != 2 {
		t.Errorf("after reopen: %d cookies", n)
	}
	j2.now = func() time.Time { return now.Add(2 * time.Hour) }
	if got := j2.Cookies("dev"); len(got) != 1 || got[0].Name != "sid" {
		t.Errorf("after expiry: %+v", got)
	}
	// Environments are isolated.
	if got := j2.Cookies("staging"); len(got) != 0 {
		t.Errorf("staging = %+v", got)
	}
	if names := j2.EnvNames(); len(names) != 1 || names[0] != "dev" {
		t.Errorf("env names = %v", names)
	}

	// Clearing one environment leaves the file for the others, clearing
	// all removes it.
	j2.HTTP("staging").SetCookies(mustURL("http://s.example.com/"), []*http.Cookie{{Name: "a", Value: "1"}})
	j2.Clear("dev")
	if err := j2.Save(); err != nil {
		t.Fatal(err)
	}
	j3, _ := OpenJar(root)
	if len(j3.Cookies("dev")) != 0 || len(j3.Cookies("staging")) != 1 {
		t.Errorf("after clear dev: dev=%v staging=%v", j3.Cookies("dev"), j3.Cookies("staging"))
	}
	j3.Clear("*")
	if err := j3.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, Dir, CookieFile)); !os.IsNotExist(err) {
		t.Errorf("an empty jar leaves no file: %v", err)
	}
}

func TestMemoryJarWritesNothing(t *testing.T) {
	j := NewMemoryJar()
	j.HTTP("").SetCookies(mustURL("http://x/"), []*http.Cookie{{Name: "a", Value: "1"}})
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	if got := j.Cookies(""); len(got) != 1 || got[0].Domain != "x" || got[0].ExpiryText(time.Now()) != "until cleared" {
		t.Errorf("cookies = %+v", got)
	}
	if defaultPath("") != "/" || defaultPath("/a") != "/" || defaultPath("/a/b") != "/a" || defaultPath("x") != "/" {
		t.Error("default path")
	}
}

func TestSaveIsANoOpWhenNothingChanged(t *testing.T) {
	root := t.TempDir()
	j, _ := OpenJar(root)
	j.HTTP("dev").SetCookies(mustURL("http://x/"), []*http.Cookie{{Name: "a", Value: "1"}})
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, Dir, CookieFile)
	before, _ := os.Stat(path)
	if err := os.Chtimes(path, before.ModTime().Add(-time.Hour), before.ModTime().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime().Add(-time.Hour)) {
		t.Error("a clean jar was rewritten")
	}
	j.Clear("dev")
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("clearing marks the jar dirty")
	}
}
