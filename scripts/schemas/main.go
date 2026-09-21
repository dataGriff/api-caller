// Command schemas generates the JSON schemas for the files apic reads and
// writes: apic.yaml, http-client.env.json (and the private file, same
// shape) and .apic/session.json. They are published on the docs site so
// editors validate and complete the files, and the VS Code extension
// registers them directly.
//
// The apic.yaml schema is built by reflecting over project.Config, so a new
// key cannot be added to the struct without appearing here; the generator
// fails if a key has no description in the table below. Run it with
// `task schemas`; TestSchemasAreCurrent fails when docs/schemas is stale.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/dataGriff/api-caller/internal/project"
)

const (
	draft   = "https://json-schema.org/draft/2020-12/schema"
	baseURL = "https://datagriff.github.io/api-caller/schemas/"
)

// descriptions documents every key of apic.yaml by its dotted path. The
// generator refuses to run when a struct field is missing here.
var descriptions = map[string]string{
	"env":                    "Environment from http-client.env.json to use when --env is not given.",
	"dir":                    "Subdirectory of the project root to scan for .http and .rest files. Default: the root itself.",
	"timeout":                "Default request timeout as a Go duration, for example 10s or 1m30s. Default 30s. `--timeout` and `# @timeout` override it.",
	"retry":                  "Default retry policy for requests without `# @retry`: `<attempts> [interval]`, for example `10 2s`. A request is re-sent until its assertions pass or the attempts are spent; the interval is a Go duration and defaults to 1s. `--retry` overrides it and `--no-retry` switches retries off.",
	"proxy":                  "Send every request through this proxy: an http, https or socks5 URL, with credentials in the userinfo if the proxy needs them. Beats HTTP_PROXY and HTTPS_PROXY; `--proxy` beats it for one command and `--no-proxy` sends directly.",
	"noProxy":                "Hosts that bypass `proxy`: a host name, `host:port`, `.example.com` for a domain and everything under it, an IP or a CIDR, or `*` for everything.",
	"cookies":                "Keep a cookie jar: cookies a response sets are sent with later requests to the same site and stored per environment in .apic/cookies.json, like captured values. Off by default; `--cookies` switches it on for one command and `# @no-cookies` exempts a request.",
	"maxBodyBytes":           "Largest response body apic reads into memory, in bytes. Default 67108864 (64 MiB); a larger response fails the request.",
	"auth":                   "Project-wide authentication defaults; see the Authentication guide.",
	"auth.default":           "An auth spec applied to every request without its own `# @auth`, for example `aws region=eu-west-2` or `bearer {{token}}`. May use {{variables}}.",
	"auth.allowExec":         "Permit `# @auth exec ...`, which runs a command from a request file. Off by default because agents edit request files.",
	"test":                   "Defaults for `apic test`.",
	"test.paths":             "Feature files or directories `apic test` runs when none are given, relative to the project root. Default: features.",
	"tls":                    "TLS settings: a private CA to trust and a client certificate to present (mTLS). Paths are relative to the project root. `--cacert`, `--cert` and `--key` override them for one command; a JetBrains `SSLConfiguration` block in the env files is read too.",
	"tls.caFile":             "PEM file with certificates to trust in addition to the system roots, for an API behind a private CA.",
	"tls.certFile":           "PEM client certificate presented to servers that ask for one.",
	"tls.keyFile":            "PEM private key for certFile; defaults to certFile when both are in one file. Refused when world-readable.",
	"tls.verifyHost":         "Verify the server's certificate. Default true; `--insecure` turns it off for one command.",
	"tls.hosts":              "Overrides for particular hosts, by host name (`api.internal.example.com`) or wildcard (`*.internal.example.com`).",
	"tls.hosts.*":            "The override for one host; unset keys fall back to the settings above.",
	"tls.hosts.*.caFile":     "PEM file with certificates to trust for this host.",
	"tls.hosts.*.certFile":   "PEM client certificate for this host.",
	"tls.hosts.*.keyFile":    "PEM private key for this host's certFile.",
	"tls.hosts.*.verifyHost": "Verify this host's certificate. Default true.",
}

// examples adds example values to a few keys, for editor completion.
var examples = map[string][]any{
	"env":          {"dev", "staging"},
	"dir":          {"api", "requests"},
	"timeout":      {"10s", "1m"},
	"retry":        {"10 2s", "5 500ms"},
	"auth.default": {"bearer {{token}}", "aws service=execute-api region=eu-west-2"},
	"proxy":        {"http://proxy.internal:3128", "socks5://127.0.0.1:1080"},
	"noProxy":      {[]string{"localhost", ".internal"}},
	"tls.caFile":   {"certs/internal-ca.pem"},
	"tls.certFile": {"certs/client.pem"},
	"tls.keyFile":  {"certs/client-key.pem"},
	"test.paths":   {[]string{"features", "smoke.feature"}},
}

// Dirs are where the schemas live: the docs site publishes them at the
// URLs the schema ids name, and the VS Code extension bundles a copy so
// they validate offline.
var Dirs = []string{"docs/schemas", "editors/vscode/schemas"}

func main() {
	out := flag.String("out", "", "one directory to write the schemas into (default: every directory in Dirs)")
	flag.Parse()
	dirs := Dirs
	if *out != "" {
		dirs = []string{*out}
	}
	for _, dir := range dirs {
		if err := write(dir); err != nil {
			log.Fatalf("schemas: %v", err)
		}
	}
}

// Files returns every schema by file name.
func Files() (map[string]*jsonschema.Schema, error) {
	cfg, err := configSchema()
	if err != nil {
		return nil, err
	}
	return map[string]*jsonschema.Schema{
		"apic.schema.json":            cfg,
		"http-client.env.schema.json": envSchema(),
		"session.schema.json":         sessionSchema(),
	}, nil
}

// Render marshals a schema the way the files on disk hold it.
func Render(s *jsonschema.Schema) ([]byte, error) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func write(dir string) error {
	files, err := Files()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // a docs directory
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := Render(files[name])
		if err != nil {
			return err
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // a committed docs asset
			return err
		}
		fmt.Println("wrote", path)
	}
	return nil
}

// configSchema reflects over project.Config using its yaml tags.
func configSchema() (*jsonschema.Schema, error) {
	s, err := structSchema(reflect.TypeOf(project.Config{}), "")
	if err != nil {
		return nil, err
	}
	s.Schema = draft
	s.ID = baseURL + "apic.schema.json"
	s.Title = "apic.yaml"
	s.Description = "Per-project defaults for apic, read from apic.yaml in the project root. Every key is optional."
	return s, nil
}

func structSchema(t reflect.Type, prefix string) (*jsonschema.Schema, error) {
	s := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}, AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		desc, ok := descriptions[path]
		if !ok {
			return nil, fmt.Errorf("apic.yaml key %q (project.Config field %s) has no description in scripts/schemas", path, f.Name)
		}
		prop, err := fieldSchema(f.Type, path)
		if err != nil {
			return nil, err
		}
		prop.Description = desc
		if ex, ok := examples[path]; ok {
			prop.Examples = ex
		}
		s.Properties[name] = prop
	}
	return s, nil
}

func fieldSchema(t reflect.Type, path string) (*jsonschema.Schema, error) {
	switch t.Kind() {
	case reflect.String:
		s := &jsonschema.Schema{Type: "string"}
		if path == "timeout" {
			s.Pattern = `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`
		}
		return s, nil
	case reflect.Bool:
		return &jsonschema.Schema{Type: "boolean"}, nil
	case reflect.Int, reflect.Int64:
		min := 0.0
		return &jsonschema.Schema{Type: "integer", Minimum: &min}, nil
	case reflect.Slice:
		item, err := fieldSchema(t.Elem(), path)
		if err != nil {
			return nil, err
		}
		return &jsonschema.Schema{Type: "array", Items: item}, nil
	case reflect.Struct:
		return structSchema(t, path)
	case reflect.Pointer:
		return fieldSchema(t.Elem(), path)
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			break
		}
		desc, ok := descriptions[path+".*"]
		if !ok {
			return nil, fmt.Errorf("apic.yaml key %q has no description in scripts/schemas", path+".*")
		}
		item, err := fieldSchema(t.Elem(), path+".*")
		if err != nil {
			return nil, err
		}
		item.Description = desc
		return &jsonschema.Schema{Type: "object", AdditionalProperties: item}, nil
	}
	return nil, fmt.Errorf("apic.yaml key %q: unsupported Go type %s", path, t)
}

// envSchema describes http-client.env.json and http-client.private.env.json:
// environment names mapping to variables. Values are stringified when
// read, so numbers, booleans and null are accepted.
func envSchema() *jsonschema.Schema {
	// A schema is a tree, so the value schema used in two places lives in
	// $defs and is referenced from each.
	value := &jsonschema.Schema{Ref: "#/$defs/value"}
	// The same reference twice would break the tree, so each use gets its own node.
	sslRef := func() *jsonschema.Schema { return &jsonschema.Schema{Ref: "#/$defs/sslConfiguration"} }
	return &jsonschema.Schema{
		Schema:      draft,
		ID:          baseURL + "http-client.env.schema.json",
		Title:       "http-client.env.json",
		Description: "Per-environment variables for .http files, the format shared by JetBrains HTTP Client, kulala.nvim, httpyac and apic. The same shape is used by http-client.private.env.json, which holds secrets and is gitignored.",
		Type:        "object",
		Defs: map[string]*jsonschema.Schema{
			"value": {
				Types:       []string{"string", "number", "boolean", "null"},
				Description: "A variable value. Non-strings are converted to text when substituted.",
			},
			"sslConfiguration": {
				Type:        "object",
				Description: "JetBrains HTTP Client's TLS block: a client certificate to present and whether to verify the server. Not a variable. Paths are relative to the project root.",
				Properties: map[string]*jsonschema.Schema{
					"clientCertificate": {
						Description: "The client certificate: a PEM path, or an object with `path` and `keyPath`.",
						OneOf: []*jsonschema.Schema{
							{Type: "string"},
							{Type: "object", Required: []string{"path"}, Properties: map[string]*jsonschema.Schema{
								"path":    {Type: "string", Description: "PEM client certificate."},
								"keyPath": {Type: "string", Description: "PEM private key; defaults to path."},
								"format":  {Type: "string", Description: "Accepted for compatibility; apic reads PEM only."},
							}},
						},
					},
					"hasCertificatePassphrase": {Type: "boolean", Description: "The key is encrypted. apic cannot prompt for a passphrase and refuses such a key."},
					"verifyHostCertificate":    {Type: "boolean", Description: "Verify the server's certificate. Default true."},
				},
			},
		},
		Properties: map[string]*jsonschema.Schema{
			"$shared": {
				Type:                 "object",
				Description:          "Variables that apply to every environment; an environment's own value wins.",
				Properties:           map[string]*jsonschema.Schema{"SSLConfiguration": sslRef()},
				AdditionalProperties: value,
			},
		},
		AdditionalProperties: &jsonschema.Schema{
			Type:                 "object",
			Description:          "The variables of one environment.",
			Properties:           map[string]*jsonschema.Schema{"SSLConfiguration": sslRef()},
			AdditionalProperties: &jsonschema.Schema{Ref: "#/$defs/value"},
			PropertyNames:        &jsonschema.Schema{Pattern: `^[A-Za-z_$][\w.-]*$`},
		},
		Examples: []any{map[string]any{
			"$shared": map[string]any{"userId": 42},
			"dev":     map[string]any{"baseUrl": "https://dev.example.com"},
			"prod":    map[string]any{"baseUrl": "https://api.example.com"},
		}},
	}
}

// sessionSchema describes .apic/session.json, which apic writes itself.
func sessionSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Schema:      draft,
		ID:          baseURL + "session.schema.json",
		Title:       ".apic/session.json",
		Description: "Values captured by `# @capture` and tokens cached by `# @auth`, per environment. Written by apic; `apic session` shows it and `apic session clear` empties it.",
		Type:        "object",
		Required:    []string{"envs"},
		Properties: map[string]*jsonschema.Schema{
			"envs": {
				Type:        "object",
				Description: "Environment name (or `default` when none was selected) to its captured values.",
				AdditionalProperties: &jsonschema.Schema{
					Type:                 "object",
					Description:          "Captured values by name. Keys starting with `$oauth2:` or `$exec:` hold cached tokens as JSON text.",
					AdditionalProperties: &jsonschema.Schema{Type: "string"},
				},
			},
		},
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}
}
