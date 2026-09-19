package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"gopkg.in/yaml.v3"
)

// TestSchemasAreCurrent fails when docs/schemas differs from what the
// generator produces, so a new apic.yaml key cannot land without `task
// schemas`.
func TestSchemasAreCurrent(t *testing.T) {
	files, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	for name, s := range files {
		want, err := Render(s)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join("..", "..", "docs", "schemas", name))
		if err != nil {
			t.Fatalf("%s: %v (run `task schemas`)", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("docs/schemas/%s is stale; run `task schemas`", name)
		}
	}
}

// resolved returns a schema ready to validate against.
func resolved(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	files, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	rs, err := files[name].Resolve(nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return rs
}

// jsonValue round-trips v through JSON so the validator sees JSON types.
func jsonValue(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestConfigSchemaAcceptsTheProjectsInTheRepo validates every apic.yaml
// under examples/ and the demo project.
func TestConfigSchemaAcceptsTheProjectsInTheRepo(t *testing.T) {
	rs := resolved(t, "apic.schema.json")
	paths, _ := filepath.Glob(filepath.Join("..", "..", "examples", "*", "apic.yaml"))
	paths = append(paths, filepath.Join("..", "..", "internal", "demoapi", "project", "apic.yaml"))
	if len(paths) < 2 {
		t.Fatalf("expected example projects, got %v", paths)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := yaml.Unmarshal(data, &v); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if err := rs.Validate(jsonValue(t, v)); err != nil {
			t.Errorf("%s does not validate: %v", p, err)
		}
	}
}

func TestConfigSchemaRejectsUnknownAndBadValues(t *testing.T) {
	rs := resolved(t, "apic.schema.json")
	good := map[string]any{
		"env": "dev", "dir": "api", "timeout": "1m30s", "maxBodyBytes": 1024,
		"auth": map[string]any{"default": "bearer {{token}}", "allowExec": true},
		"test": map[string]any{"paths": []any{"features"}},
	}
	if err := rs.Validate(jsonValue(t, good)); err != nil {
		t.Fatalf("a full config should validate: %v", err)
	}
	for name, bad := range map[string]any{
		"misspelled key":     map[string]any{"envv": "dev"},
		"nested unknown key": map[string]any{"auth": map[string]any{"deflt": "x"}},
		"bad duration":       map[string]any{"timeout": "soon"},
		"negative bytes":     map[string]any{"maxBodyBytes": -1},
		"paths not a list":   map[string]any{"test": map[string]any{"paths": "features"}},
	} {
		if err := rs.Validate(jsonValue(t, bad)); err == nil {
			t.Errorf("%s should not validate: %v", name, bad)
		}
	}
}

func TestEnvSchema(t *testing.T) {
	rs := resolved(t, "http-client.env.schema.json")
	for _, p := range []string{
		filepath.Join("..", "..", "examples", "httpbin", "http-client.env.json"),
		filepath.Join("..", "..", "examples", "spotify", "http-client.private.env.json"),
		filepath.Join("..", "..", "internal", "demoapi", "project", "http-client.private.env.json"),
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		if err := rs.Validate(v); err != nil {
			t.Errorf("%s does not validate: %v", p, err)
		}
	}
	if err := rs.Validate(jsonValue(t, map[string]any{"dev": map[string]any{"n": 1, "b": true, "z": nil, "s": "x"}})); err != nil {
		t.Errorf("scalar values should validate: %v", err)
	}
	if err := rs.Validate(jsonValue(t, map[string]any{"dev": map[string]any{"obj": map[string]any{}}})); err == nil {
		t.Error("an object value should not validate")
	}
	if err := rs.Validate(jsonValue(t, map[string]any{"dev": "not an object"})); err == nil {
		t.Error("an environment must be an object")
	}
}

func TestSessionSchema(t *testing.T) {
	rs := resolved(t, "session.schema.json")
	if err := rs.Validate(jsonValue(t, map[string]any{"envs": map[string]any{"dev": map[string]any{"token": "abc", "$oauth2:x": "{}"}}})); err != nil {
		t.Errorf("a session should validate: %v", err)
	}
	if err := rs.Validate(jsonValue(t, map[string]any{"vars": map[string]any{}})); err == nil {
		t.Error("envs is required")
	}
}

func TestEveryConfigKeyIsDocumented(t *testing.T) {
	// The generator enforces this; pin that the message names the key.
	s, err := configSchema()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"env", "dir", "timeout", "maxBodyBytes", "auth", "test"} {
		if s.Properties[key] == nil || s.Properties[key].Description == "" {
			t.Errorf("%s missing or undocumented", key)
		}
	}
	if !strings.Contains(s.Properties["auth"].Properties["allowExec"].Description, "exec") {
		t.Error("nested keys keep their descriptions")
	}
}
