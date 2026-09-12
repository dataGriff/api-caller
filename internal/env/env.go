// Package env loads environment variable files:
//
//	http-client.env.json          {"dev": {"baseUrl": "..."}, "$shared": {...}}
//	http-client.private.env.json  same shape, for secrets, gitignored
//	.env                          KEY=value lines
//
// These are the conventions used by JetBrains HTTP Client, kulala.nvim and
// httpyac, so one set of files serves every tool.
package env

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/joho/godotenv"
)

const (
	PublicFile  = "http-client.env.json"
	PrivateFile = "http-client.private.env.json"
	DotEnvFile  = ".env"
	sharedKey   = "$shared"
)

// Environments is everything loaded from the env files of a project.
type Environments struct {
	Public  map[string]map[string]string // env name -> vars
	Private map[string]map[string]string
	DotEnv  map[string]string
	Found   []string // files that were present
}

// Load reads the env files in root. Missing files are fine.
func Load(root string) (*Environments, error) {
	e := &Environments{Public: map[string]map[string]string{}, Private: map[string]map[string]string{}, DotEnv: map[string]string{}}
	var err error
	if e.Public, err = loadJSON(filepath.Join(root, PublicFile), &e.Found); err != nil {
		return nil, err
	}
	if e.Private, err = loadJSON(filepath.Join(root, PrivateFile), &e.Found); err != nil {
		return nil, err
	}
	dot := filepath.Join(root, DotEnvFile)
	if m, err := godotenv.Read(dot); err == nil {
		e.DotEnv = m
		e.Found = append(e.Found, DotEnvFile)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", DotEnvFile, err)
	}
	return e, nil
}

// Names returns the environment names found in the JSON files, sorted.
func (e *Environments) Names() []string {
	set := map[string]bool{}
	for n := range e.Public {
		set[n] = true
	}
	for n := range e.Private {
		set[n] = true
	}
	delete(set, sharedKey)
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Has reports whether an environment is declared in either JSON file.
func (e *Environments) Has(name string) bool {
	_, a := e.Public[name]
	_, b := e.Private[name]
	return a || b
}

// PublicVars returns the public vars for env merged over $shared.
func (e *Environments) PublicVars(env string) map[string]string {
	return merge(e.Public[sharedKey], e.Public[env])
}

// PrivateVars returns the private vars for env merged over $shared.
func (e *Environments) PrivateVars(env string) map[string]string {
	return merge(e.Private[sharedKey], e.Private[env])
}

func merge(base, over map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

func loadJSON(path string, found *[]string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var raw map[string]map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	for envName, vars := range raw {
		m := map[string]string{}
		for k, v := range vars {
			m[k] = stringify(v)
		}
		out[envName] = m
	}
	*found = append(*found, filepath.Base(path))
	return out, nil
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
