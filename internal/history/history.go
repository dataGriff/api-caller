// Package history keeps the last few responses of each named request, per
// environment, so "what did this return yesterday?" has an answer. It is
// off unless apic.yaml sets `history: N`. Entries live under
// .apic/history/<env>/<request>/, one JSON file each, beside the session
// and under the same rules: the directory ignores itself and the files are
// 0600, since a response may hold data nobody wants in a commit.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dataGriff/api-caller/internal/session"
)

// Dir is the history directory under the project's .apic directory.
const Dir = "history"

// DefaultEnv names the environment of a run that used none, as the
// session does.
const DefaultEnv = session.DefaultEnv

// Store is the history of one project.
type Store struct {
	root string // the .apic/history directory
	keep int
}

// New returns the store for a project root keeping keep entries per
// request and environment. Nothing is written until Record.
func New(root string, keep int) *Store {
	return &Store{root: filepath.Join(root, session.Dir, Dir), keep: keep}
}

// Keep is the number of entries kept per request and environment.
func (s *Store) Keep() int { return s.keep }

// Entry is one recorded response. Index counts from 1, the newest.
type Entry struct {
	Index      int       `json:"index"`
	Time       time.Time `json:"time"`
	OK         bool      `json:"ok"`
	Status     int       `json:"status"`
	StatusText string    `json:"status_text,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	Size       int       `json:"size"`
	// File is the entry's path relative to the project root.
	File string `json:"file"`
	// Result is the run's result as `apic run --json` prints it, with
	// sensitive headers masked and, for a --redact run, every value.
	Result json.RawMessage `json:"-"`
}

// file is the on-disk form of an entry.
type file struct {
	Time    time.Time       `json:"time"`
	Env     string          `json:"env"`
	Request string          `json:"request"`
	Result  json.RawMessage `json:"result"`
}

// summary is the part of a stored result the listing reads.
type summary struct {
	OK       bool `json:"ok"`
	Response *struct {
		Status     int    `json:"status"`
		StatusText string `json:"status_text"`
		DurationMs int64  `json:"duration_ms"`
		Size       int    `json:"size"`
	} `json:"response"`
}

func envKey(env string) string {
	if env == "" {
		return DefaultEnv
	}
	return env
}

// component turns a request or environment name into one safe path
// segment: letters, digits, '-' and '_' stay, everything else (a dot, a
// slash, a space) is %XX-encoded, so no name can climb out of the
// directory or collide with another.
func component(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func (s *Store) dir(env, name string) string {
	return filepath.Join(s.root, component(envKey(env)), component(name))
}

// Record stores result, the JSON of a run's result, as the newest entry
// for name in env, then drops the oldest entries beyond Keep.
func (s *Store) Record(env, name string, at time.Time, result []byte) error {
	if s.keep <= 0 {
		return nil
	}
	dir := s.dir(env, name)
	if err := session.EnsureDir(filepath.Dir(s.root)); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file{Time: at.UTC(), Env: envKey(env), Request: name, Result: result}, "", "  ")
	if err != nil {
		return err
	}
	base := at.UTC().Format("20060102T150405.000000000Z")
	path := filepath.Join(dir, base+".json")
	for n := 2; ; n++ {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.json", base, n))
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return s.prune(dir)
}

// prune removes the oldest entries in dir beyond Keep.
func (s *Store) prune(dir string) error {
	names, err := entryFiles(dir)
	if err != nil {
		return err
	}
	for len(names) > s.keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}

// entryFiles lists the entry files in dir, oldest first. The names are
// UTC timestamps, so their order is the order they were written in.
func entryFiles(dir string) ([]string, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, de := range des {
		if !de.IsDir() && strings.HasSuffix(de.Name(), ".json") {
			names = append(names, de.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool { return lessName(names[i], names[j]) })
	return names, nil
}

// lessName orders entry files by time, then by the -N suffix two entries
// recorded in the same nanosecond get.
func lessName(a, b string) bool {
	ab, an := splitName(a)
	bb, bn := splitName(b)
	if ab != bb {
		return ab < bb
	}
	return an < bn
}

func splitName(name string) (string, int) {
	name = strings.TrimSuffix(name, ".json")
	base, suffix, ok := strings.Cut(name, "-")
	if !ok {
		return name, 1
	}
	n := 0
	if _, err := fmt.Sscanf(suffix, "%d", &n); err != nil {
		return name, 0
	}
	return base, n
}

// List returns the entries for name in env, newest first, without their
// results. No history is an empty list, not an error.
func (s *Store) List(env, name string) ([]Entry, error) {
	dir := s.dir(env, name)
	names, err := entryFiles(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(names))
	for i := len(names) - 1; i >= 0; i-- {
		e, err := s.read(filepath.Join(dir, names[i]))
		if err != nil {
			return nil, err
		}
		e.Index = len(out) + 1
		e.Result = nil
		out = append(out, e)
	}
	return out, nil
}

// Recorded is a request that has history in an environment.
type Recorded struct {
	Request string `json:"request"`
	Entries int    `json:"entries"`
}

// Requests lists the requests with history in env, by name.
func (s *Store) Requests(env string) ([]Recorded, error) {
	des, err := os.ReadDir(filepath.Join(s.root, component(envKey(env))))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Recorded
	for _, de := range des {
		if !de.IsDir() {
			continue
		}
		name, err := url.PathUnescape(de.Name())
		if err != nil {
			continue // not a directory apic made
		}
		files, err := entryFiles(filepath.Join(s.root, component(envKey(env)), de.Name()))
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			out = append(out, Recorded{Request: name, Entries: len(files)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Request < out[j].Request })
	return out, nil
}

// Get returns entry index (1 is the newest) for name in env, with its
// result.
func (s *Store) Get(env, name string, index int) (Entry, error) {
	dir := s.dir(env, name)
	names, err := entryFiles(dir)
	if err != nil {
		return Entry{}, err
	}
	if index < 1 || index > len(names) {
		return Entry{}, &RangeError{Name: name, Index: index, Have: len(names)}
	}
	e, err := s.read(filepath.Join(dir, names[len(names)-index]))
	if err != nil {
		return Entry{}, err
	}
	e.Index = index
	return e, nil
}

// RangeError is a Get for an entry that does not exist.
type RangeError struct {
	Name  string
	Index int
	Have  int
}

func (e *RangeError) Error() string {
	switch e.Have {
	case 0:
		return fmt.Sprintf("no history for %s", e.Name)
	case 1:
		return fmt.Sprintf("%s has 1 history entry, no #%d", e.Name, e.Index)
	}
	return fmt.Sprintf("%s has %d history entries, no #%d", e.Name, e.Have, e.Index)
}

func (s *Store) read(path string) (Entry, error) {
	data, err := os.ReadFile(path) //nolint:gosec // a file apic wrote under .apic/history
	if err != nil {
		return Entry{}, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", path, err)
	}
	var sum summary
	if err := json.Unmarshal(f.Result, &sum); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", path, err)
	}
	e := Entry{Time: f.Time, OK: sum.OK, Result: f.Result}
	if r := sum.Response; r != nil {
		e.Status, e.StatusText, e.DurationMs, e.Size = r.Status, r.StatusText, r.DurationMs, r.Size
	}
	project := filepath.Dir(filepath.Dir(s.root))
	if rel, err := filepath.Rel(project, path); err == nil {
		e.File = filepath.ToSlash(rel)
	}
	return e, nil
}

// Clear removes the history of name in env; an empty name clears the
// whole environment, and env "*" every environment. It reports how many
// entries it removed.
func (s *Store) Clear(env, name string) (int, error) {
	var dir string
	switch {
	case env == "*":
		dir = s.root
	case name == "":
		dir = filepath.Join(s.root, component(envKey(env)))
	default:
		dir = s.dir(env, name)
	}
	n := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".json") {
			n++
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return n, os.RemoveAll(dir)
}
