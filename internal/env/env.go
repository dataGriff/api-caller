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
	// auth holds each file's Security.Auth blocks: file -> env -> name.
	auth map[string]map[string]map[string]map[string]any
}

// AuthConfig is one entry of a JetBrains `Security.Auth` block:
//
//	"Security": {
//	  "Auth": {
//	    "my-api": {"Type": "OAuth2", "Grant Type": "Client Credentials",
//	               "Token URL": "https://idp/token", "Client ID": "{{clientId}}"}
//	  }
//	}
//
// Requests use it as {{$auth.token("my-api")}}. Fields are as written, so
// strings may hold {{placeholders}}; the private file's fields win over
// the public file's, and an environment's over $shared's.
type AuthConfig struct {
	Name   string
	Fields map[string]any
	Files  []string // the env files that declare it
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
	e.auth = map[string]map[string]map[string]map[string]any{PublicFile: {}, PrivateFile: {}}
	if e.Public, err = loadJSON(filepath.Join(root, PublicFile), &e.Found, e.ssl, e.auth[PublicFile]); err != nil {
		return nil, err
	}
	if e.Private, err = loadJSON(filepath.Join(root, PrivateFile), &e.Found, e.ssl, e.auth[PrivateFile]); err != nil {
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

// Auth returns the Security.Auth configurations in effect for env, by
// name: $shared's and env's from the public file, then the private file's
// over them field by field.
func (e *Environments) Auth(env string) map[string]*AuthConfig {
	out := map[string]*AuthConfig{}
	for _, file := range []string{PublicFile, PrivateFile} {
		layers := []string{sharedKey}
		if env != "" && env != sharedKey {
			layers = append(layers, env)
		}
		for _, layer := range layers {
			for name, fields := range e.auth[file][layer] {
				c := out[name]
				if c == nil {
					c = &AuthConfig{Name: name, Fields: map[string]any{}}
					out[name] = c
				}
				for k, v := range fields {
					c.Fields[k] = v
				}
				if len(c.Files) == 0 || c.Files[len(c.Files)-1] != file {
					c.Files = append(c.Files, file)
				}
			}
		}
	}
	return out
}

// AuthNames is every Security.Auth name any environment declares, sorted.
func (e *Environments) AuthNames() []string {
	set := map[string]bool{}
	for _, envs := range e.auth {
		for _, byName := range envs {
			for name := range byName {
				set[name] = true
			}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// AllAuth is every Security.Auth entry as each file and environment
// declares it, for validate: file -> env -> name -> fields.
func (e *Environments) AllAuth() map[string]map[string]map[string]map[string]any {
	return e.auth
}

const (
	sslKey      = "SSLConfiguration"
	securityKey = "Security"
)

// loadJSON reads one env file. An SSLConfiguration block is lifted out of
// the variables into ssl, and a Security block's Auth entries into auth,
// later files overriding earlier ones.
func loadJSON(path string, found *[]string, ssl map[string]*SSLConfig, auth map[string]map[string]map[string]any) (map[string]map[string]string, error) {
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
			if k == securityKey {
				entries, err := parseSecurity(v)
				if err != nil {
					return nil, fmt.Errorf("%s: %s: %s: %w", filepath.Base(path), envName, securityKey, err)
				}
				auth[envName] = entries
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

// parseSecurity reads a JetBrains Security block: {"Auth": {"<name>":
// {...}}}. Only Auth is read.
func parseSecurity(v any) (map[string]map[string]any, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("expected an object")
	}
	out := map[string]map[string]any{}
	switch a := obj["Auth"].(type) {
	case nil:
	case map[string]any:
		for name, fields := range a {
			f, ok := fields.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("auth configuration %q: expected an object", name)
			}
			out[name] = f
		}
	default:
		return nil, errors.New("Auth: expected an object of named configurations")
	}
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
