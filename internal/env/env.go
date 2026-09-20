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
	ssl     map[string]*SSLConfig
}

// SSLConfig is the JetBrains `SSLConfiguration` block of an environment:
//
//	"SSLConfiguration": {
//	  "clientCertificate": {"path": "certs/client.pem", "keyPath": "certs/client-key.pem"},
//	  "hasCertificatePassphrase": false,
//	  "verifyHostCertificate": true
//	}
//
// It is read from either env file (the private one wins) and is not a
// variable. Paths are relative to the project root.
type SSLConfig struct {
	CertFile      string
	KeyFile       string
	HasPassphrase bool
	VerifyHost    *bool
}

// SSL returns the SSLConfiguration block for env, merged over $shared, or
// nil when neither file declares one.
func (e *Environments) SSL(env string) *SSLConfig {
	if c, ok := e.ssl[env]; ok && env != "" {
		return c
	}
	if c, ok := e.ssl[sharedKey]; ok {
		return c
	}
	return nil
}

// Load reads the env files in root. Missing files are fine.
func Load(root string) (*Environments, error) {
	e := &Environments{Public: map[string]map[string]string{}, Private: map[string]map[string]string{}, DotEnv: map[string]string{}}
	var err error
	e.ssl = map[string]*SSLConfig{}
	if e.Public, err = loadJSON(filepath.Join(root, PublicFile), &e.Found, e.ssl); err != nil {
		return nil, err
	}
	if e.Private, err = loadJSON(filepath.Join(root, PrivateFile), &e.Found, e.ssl); err != nil {
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

const sslKey = "SSLConfiguration"

// loadJSON reads one env file. An SSLConfiguration block is lifted out of
// the variables into ssl, later files overriding earlier ones.
func loadJSON(path string, found *[]string, ssl map[string]*SSLConfig) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	data, err := os.ReadFile(path) //nolint:gosec // reading the project's env file by path is the whole job
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
			if k == sslKey {
				c, err := parseSSL(v)
				if err != nil {
					return nil, fmt.Errorf("%s: %s: %s: %w", filepath.Base(path), envName, sslKey, err)
				}
				ssl[envName] = c
				continue
			}
			m[k] = stringify(v)
		}
		out[envName] = m
	}
	*found = append(*found, filepath.Base(path))
	return out, nil
}

// parseSSL reads a JetBrains SSLConfiguration value. clientCertificate is
// an object with path and keyPath, or a bare path string.
func parseSSL(v any) (*SSLConfig, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("expected an object")
	}
	c := &SSLConfig{}
	switch cert := obj["clientCertificate"].(type) {
	case nil:
	case string:
		c.CertFile = cert
	case map[string]any:
		c.CertFile, _ = cert["path"].(string)
		c.KeyFile, _ = cert["keyPath"].(string)
		if c.CertFile == "" {
			return nil, errors.New("clientCertificate needs a path")
		}
	default:
		return nil, errors.New("clientCertificate must be a path or an object with path and keyPath")
	}
	if b, ok := obj["hasCertificatePassphrase"].(bool); ok {
		c.HasPassphrase = b
	}
	if b, ok := obj["verifyHostCertificate"].(bool); ok {
		c.VerifyHost = &b
	}
	return c, nil
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
