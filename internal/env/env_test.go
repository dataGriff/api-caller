package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestLoadMissingFilesIsFine pins that a project with no env files at all
// still loads: every file here is optional.
func TestLoadMissingFilesIsFine(t *testing.T) {
	e, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("missing env files should not be an error: %v", err)
	}
	if len(e.Names()) != 0 || len(e.Found) != 0 {
		t.Errorf("expected nothing found, got names=%v found=%v", e.Names(), e.Found)
	}
	if e.Public == nil || e.Private == nil || e.DotEnv == nil {
		t.Error("maps should be initialised, not nil")
	}
	// Reading an absent environment is a lookup miss, not a panic.
	if len(e.PublicVars("dev")) != 0 || len(e.PrivateVars("dev")) != 0 {
		t.Error("an unknown environment should resolve to nothing")
	}
}

func TestLoadReadsAllThreeFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, PublicFile, `{"dev": {"baseUrl": "https://dev.example.com"}}`)
	write(t, dir, PrivateFile, `{"dev": {"token": "secret-token"}}`)
	write(t, dir, DotEnvFile, "API_KEY=from-dotenv\n")

	e, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.PublicVars("dev")["baseUrl"]; got != "https://dev.example.com" {
		t.Errorf("public baseUrl = %q", got)
	}
	if got := e.PrivateVars("dev")["token"]; got != "secret-token" {
		t.Errorf("private token = %q", got)
	}
	if got := e.DotEnv["API_KEY"]; got != "from-dotenv" {
		t.Errorf("dotenv API_KEY = %q", got)
	}
	if len(e.Found) != 3 {
		t.Errorf("Found should list all three files, got %v", e.Found)
	}
}

// TestSharedMerging covers $shared, which is the convention apic inherits
// from JetBrains: per-environment values win over shared ones, and $shared is
// never itself an environment.
func TestSharedMerging(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, PublicFile, `{
		"$shared": {"baseUrl": "https://shared.example.com", "common": "yes"},
		"dev":     {"baseUrl": "https://dev.example.com"},
		"prod":    {}
	}`)
	write(t, dir, PrivateFile, `{
		"$shared": {"token": "shared-token"},
		"dev":     {"token": "dev-token"}
	}`)

	e, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	dev := e.PublicVars("dev")
	if dev["baseUrl"] != "https://dev.example.com" {
		t.Errorf("dev should win over $shared, got %q", dev["baseUrl"])
	}
	if dev["common"] != "yes" {
		t.Errorf("dev should inherit $shared, got %q", dev["common"])
	}
	if got := e.PublicVars("prod")["baseUrl"]; got != "https://shared.example.com" {
		t.Errorf("prod should fall back to $shared, got %q", got)
	}
	if got := e.PrivateVars("dev")["token"]; got != "dev-token" {
		t.Errorf("private dev should win over $shared, got %q", got)
	}
	if got := e.PrivateVars("prod")["token"]; got != "shared-token" {
		t.Errorf("private prod should fall back to $shared, got %q", got)
	}

	// $shared is a merge source, not an environment anyone can select.
	for _, n := range e.Names() {
		if n == sharedKey {
			t.Fatalf("%s should not be listed as an environment: %v", sharedKey, e.Names())
		}
	}
}

// TestMergedVarsAreCopies pins that callers cannot mutate the loaded state
// through the map they are handed.
func TestMergedVarsAreCopies(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, PublicFile, `{"dev": {"baseUrl": "https://dev.example.com"}}`)
	e, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	vars := e.PublicVars("dev")
	vars["baseUrl"] = "mutated"
	if got := e.PublicVars("dev")["baseUrl"]; got != "https://dev.example.com" {
		t.Errorf("PublicVars should hand out a copy, got %q", got)
	}
}

func TestNamesAndHas(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, PublicFile, `{"prod": {}, "dev": {}, "$shared": {}}`)
	write(t, dir, PrivateFile, `{"dev": {}, "staging": {}}`)

	e, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := e.Names()
	want := []string{"dev", "prod", "staging"}
	if len(got) != len(want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names = %v, want %v (sorted, deduplicated, no %s)", got, want, sharedKey)
		}
	}
	for _, n := range want {
		if !e.Has(n) {
			t.Errorf("Has(%q) = false", n)
		}
	}
	if e.Has("nope") {
		t.Error(`Has("nope") = true`)
	}
}

// TestNonStringValues covers the JSON types a hand-written env file can
// legally contain; apic substitutes strings, so each has to render sensibly.
func TestNonStringValues(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, PublicFile, `{"dev": {
		"str":   "s",
		"int":   8080,
		"float": 1.5,
		"bool":  true,
		"null":  null,
		"list":  [1, 2],
		"obj":   {"a": 1}
	}}`)
	e, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	vars := e.PublicVars("dev")
	for name, want := range map[string]string{
		"str":   "s",
		"int":   "8080", // not 8080.000000, and not 8.08e+03
		"float": "1.5",
		"bool":  "true",
		"null":  "",
		"list":  "[1,2]",
		"obj":   `{"a":1}`,
	} {
		if got := vars[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// TestBadJSONNamesTheFile pins that a typo in an env file produces an error
// the user can act on, naming the file rather than just "unexpected token".
func TestBadJSONNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, PublicFile, `{"dev": {"baseUrl": }}`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("malformed env JSON should be an error")
	}
	if !strings.Contains(err.Error(), PublicFile) {
		t.Errorf("error should name the file, got %q", err)
	}
}

func TestBadPrivateJSONNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, PrivateFile, `not json at all`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("malformed private env JSON should be an error")
	}
	if !strings.Contains(err.Error(), PrivateFile) {
		t.Errorf("error should name the file, got %q", err)
	}
}

func TestSSLConfigurationIsReadNotAVariable(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, PublicFile, `{"$shared": {"SSLConfiguration": {"clientCertificate": "certs/shared.pem"}}, "dev": {"baseUrl": "https://dev", "SSLConfiguration": {"clientCertificate": {"path": "certs/dev.pem", "keyPath": "certs/dev-key.pem"}, "verifyHostCertificate": false}}, "prod": {"baseUrl": "https://prod"}}`)
	write(t, dir, PrivateFile, `{"prod": {"SSLConfiguration": {"clientCertificate": {"path": "certs/prod.pem"}, "hasCertificatePassphrase": true}}}`)
	e, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e.PublicVars("dev")["SSLConfiguration"]; ok {
		t.Error("SSLConfiguration leaked into the variables")
	}
	dev := e.SSL("dev")
	if dev == nil || dev.CertFile != "certs/dev.pem" || dev.KeyFile != "certs/dev-key.pem" || dev.VerifyHost == nil || *dev.VerifyHost {
		t.Errorf("dev = %+v", dev)
	}
	prod := e.SSL("prod")
	if prod == nil || prod.CertFile != "certs/prod.pem" || !prod.HasPassphrase || prod.VerifyHost != nil {
		t.Errorf("prod (private file) = %+v", prod)
	}
	if other := e.SSL("staging"); other == nil || other.CertFile != "certs/shared.pem" {
		t.Errorf("$shared fallback = %+v", other)
	}
	write(t, dir, PrivateFile, `{"prod": {"SSLConfiguration": {"clientCertificate": 42}}}`)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "clientCertificate") {
		t.Errorf("bad block: %v", err)
	}
}
