// Package session persists values captured from responses between apic
// invocations, so `apic run login` followed by `apic run get-user` works the
// way it does in a GUI client. State lives in .apic/session.json under the
// project root; the directory ignores itself via .apic/.gitignore.
package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const (
	Dir        = ".apic"
	File       = "session.json"
	DefaultEnv = "default"
)

// Store is the on-disk session.
type Store struct {
	path string
	Envs map[string]map[string]string `json:"envs"`
}

// Open loads the session for a project root, or an empty one.
func Open(root string) (*Store, error) {
	s := &Store{path: filepath.Join(root, Dir, File), Envs: map[string]map[string]string{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	if s.Envs == nil {
		s.Envs = map[string]map[string]string{}
	}
	return s, nil
}

func key(env string) string {
	if env == "" {
		return DefaultEnv
	}
	return env
}

// Vars returns a copy of the captured values for env.
func (s *Store) Vars(env string) map[string]string {
	out := map[string]string{}
	for k, v := range s.Envs[key(env)] {
		out[k] = v
	}
	return out
}

// Get returns one captured value.
func (s *Store) Get(env, name string) (string, bool) {
	v, ok := s.Envs[key(env)][name]
	return v, ok
}

// Set merges vars into env's captured values (in memory; call Save).
func (s *Store) Set(env string, vars map[string]string) {
	k := key(env)
	if s.Envs[k] == nil {
		s.Envs[k] = map[string]string{}
	}
	for n, v := range vars {
		s.Envs[k][n] = v
	}
}

// Clear drops captured values for env, or for every env when env is "*".
func (s *Store) Clear(env string) {
	if env == "*" {
		s.Envs = map[string]map[string]string{}
		return
	}
	delete(s.Envs, key(env))
}

// EnvNames lists the environments that have captured values.
func (s *Store) EnvNames() []string {
	var names []string
	for n, vars := range s.Envs {
		if len(vars) > 0 {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// Save writes the session to disk, creating .apic/ and its .gitignore.
func (s *Store) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, fs.ErrNotExist) {
		_ = os.WriteFile(gi, []byte("# created by apic; session state must not be committed\n*\n"), 0o600)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o600)
}
