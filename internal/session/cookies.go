package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CookieFile holds the cookie jar, beside session.json.
const CookieFile = "cookies.json"

// Cookie is one stored cookie. Domain, Path and HostOnly are the jar's
// view after the RFC 6265 defaults were applied, so a saved cookie is sent
// to the same hosts after a restart as before it. A zero Expires means the
// cookie lives until `apic session clear`: apic keeps a login's session
// cookie for the same reason it keeps a captured token.
type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitzero"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"http_only,omitempty"`
	HostOnly bool      `json:"host_only,omitempty"`
}

func (c Cookie) key() string { return c.Domain + "|" + c.Path + "|" + c.Name }

// Jar is the cookie jar of a project, one set of cookies per environment,
// stored in .apic/cookies.json next to the session. The runtime behaviour
// is net/http/cookiejar's, without a public-suffix list: a project that
// talks to one API has no need for one, and it keeps the binary small.
type Jar struct {
	path  string
	mu    sync.Mutex
	envs  map[string][]Cookie
	live  map[string]*EnvJar
	now   func() time.Time
	dirty bool // something changed since the last Save
}

// NewMemoryJar returns a jar that is never written to disk.
func NewMemoryJar() *Jar {
	return &Jar{envs: map[string][]Cookie{}, live: map[string]*EnvJar{}, now: time.Now}
}

// OpenJar loads the jar for a project root, or an empty one.
func OpenJar(root string) (*Jar, error) {
	j := NewMemoryJar()
	j.path = filepath.Join(root, Dir, CookieFile)
	data, err := os.ReadFile(j.path)
	if errors.Is(err, fs.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return nil, err
	}
	var file struct {
		Envs map[string][]Cookie `json:"envs"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", j.path, err)
	}
	for env, cookies := range file.Envs {
		j.envs[env] = j.unexpired(cookies)
	}
	return j, nil
}

// HTTP returns the http.CookieJar for env, which records what the server
// sets so it can be saved.
func (j *Jar) HTTP(env string) *EnvJar {
	j.mu.Lock()
	defer j.mu.Unlock()
	k := key(env)
	if e, ok := j.live[k]; ok {
		return e
	}
	inner, _ := cookiejar.New(nil) // never fails with a nil PublicSuffixList
	e := &EnvJar{parent: j, env: k, inner: inner, records: map[string]Cookie{}}
	for _, c := range j.envs[k] {
		e.records[c.key()] = c
		inner.SetCookies(c.url(), []*http.Cookie{c.http()})
	}
	j.live[k] = e
	return e
}

// Cookies lists the unexpired cookies stored for env, sorted by domain,
// path and name.
func (j *Jar) Cookies(env string) []Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.unexpired(j.envs[key(env)])
	sort.Slice(out, func(a, b int) bool {
		if out[a].Domain != out[b].Domain {
			return out[a].Domain < out[b].Domain
		}
		if out[a].Path != out[b].Path {
			return out[a].Path < out[b].Path
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// EnvNames lists the environments that have cookies.
func (j *Jar) EnvNames() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	var names []string
	for n, cookies := range j.envs {
		if len(j.unexpired(cookies)) > 0 {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Clear drops the cookies of env, or of every env when env is "*".
func (j *Jar) Clear(env string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.dirty = true
	if env == "*" {
		j.envs = map[string][]Cookie{}
		j.live = map[string]*EnvJar{}
		return
	}
	delete(j.envs, key(env))
	delete(j.live, key(env))
}

// Save writes the jar to disk with the same protections as the session
// file. A memory-only jar is a no-op.
func (j *Jar) Save() error {
	if j.path == "" {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.dirty {
		return nil
	}
	j.dirty = false
	envs := map[string][]Cookie{}
	for env, cookies := range j.envs {
		if live := j.unexpired(cookies); len(live) > 0 {
			envs[env] = live
		}
	}
	if len(envs) == 0 {
		if err := os.Remove(j.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := ensureDir(filepath.Dir(j.path)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Envs map[string][]Cookie `json:"envs"`
	}{envs}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(j.path, append(data, '\n'), 0o600)
}

func (j *Jar) unexpired(cookies []Cookie) []Cookie {
	now := j.now()
	out := make([]Cookie, 0, len(cookies))
	for _, c := range cookies {
		if !c.Expires.IsZero() && !c.Expires.After(now) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ensureDir creates .apic/ with its .gitignore, as Store.Save does.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(gi, []byte("# created by apic; session state must not be committed\n*\n"), 0o600); err != nil {
			return fmt.Errorf("%s: %w", gi, err)
		}
	}
	return nil
}

// EnvJar is the http.CookieJar of one environment. It wraps a
// net/http/cookiejar and records every cookie the server sets, since the
// standard jar cannot be enumerated for saving.
type EnvJar struct {
	parent  *Jar
	env     string
	inner   *cookiejar.Jar
	records map[string]Cookie
}

// Cookies returns the cookies to send with a request to u.
func (e *EnvJar) Cookies(u *url.URL) []*http.Cookie {
	return e.inner.Cookies(u)
}

// SetCookies stores the cookies a response to u set, and records the ones
// the jar accepted so they survive the process.
func (e *EnvJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	e.inner.SetCookies(u, cookies)
	e.parent.mu.Lock()
	defer e.parent.mu.Unlock()
	now := e.parent.now()
	for _, c := range cookies {
		rec := Cookie{Name: c.Name, Value: c.Value, Secure: c.Secure, HTTPOnly: c.HttpOnly}
		rec.Domain = strings.ToLower(strings.TrimPrefix(c.Domain, "."))
		if rec.Domain == "" {
			rec.Domain, rec.HostOnly = strings.ToLower(u.Hostname()), true
		}
		rec.Path = c.Path
		if rec.Path == "" || !strings.HasPrefix(rec.Path, "/") {
			rec.Path = defaultPath(u.Path)
		}
		switch {
		case c.MaxAge < 0:
			delete(e.records, rec.key())
			continue
		case c.MaxAge > 0:
			rec.Expires = now.Add(time.Duration(c.MaxAge) * time.Second)
		case !c.Expires.IsZero():
			rec.Expires = c.Expires
		}
		if !rec.Expires.IsZero() && !rec.Expires.After(now) {
			delete(e.records, rec.key())
			continue
		}
		// Only what the jar accepted (domain rules, secure scheme) is
		// worth keeping: ask it back for the cookie's own scope.
		if !e.accepted(rec) {
			continue
		}
		e.records[rec.key()] = rec
	}
	list := make([]Cookie, 0, len(e.records))
	for _, c := range e.records {
		list = append(list, c)
	}
	e.parent.envs[e.env] = list
	e.parent.dirty = true
}

func (e *EnvJar) accepted(rec Cookie) bool {
	for _, c := range e.inner.Cookies(rec.url()) {
		if c.Name == rec.Name && c.Value == rec.Value {
			return true
		}
	}
	return false
}

// url is a request URL inside the cookie's scope, for replaying it into a
// jar and for asking the jar whether it holds it.
func (c Cookie) url() *url.URL {
	scheme := "http"
	if c.Secure {
		scheme = "https"
	}
	return &url.URL{Scheme: scheme, Host: c.Domain, Path: c.Path}
}

func (c Cookie) http() *http.Cookie {
	out := &http.Cookie{Name: c.Name, Value: c.Value, Path: c.Path, Expires: c.Expires, Secure: c.Secure, HttpOnly: c.HTTPOnly}
	if !c.HostOnly {
		out.Domain = c.Domain
	}
	return out
}

// defaultPath is the RFC 6265 section 5.1.4 default-path of a request
// path, which a cookie without a Path attribute takes.
func defaultPath(p string) string {
	if p == "" || p[0] != '/' {
		return "/"
	}
	i := strings.LastIndexByte(p, '/')
	if i == 0 {
		return "/"
	}
	return p[:i]
}

// ExpiryText describes when a cookie expires, for listings.
func (c Cookie) ExpiryText(now time.Time) string {
	if c.Expires.IsZero() {
		return "until cleared"
	}
	d := c.Expires.Sub(now).Round(time.Second)
	if d <= 0 {
		return "expired"
	}
	return "expires in " + d.String()
}
