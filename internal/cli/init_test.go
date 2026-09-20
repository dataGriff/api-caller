package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
)

// TestInitGitignoresEveryCredentialFile pins that `apic init` ignores each
// file apic itself treats as a secret source. .env was missing, so a project
// scaffolded by apic would happily commit it.
func TestInitGitignoresEveryCredentialFile(t *testing.T) {
	dir := t.TempDir()
	app := New()
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr
	if code := app.Execute(context.Background(), []string{"init", dir}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("init should write a .gitignore: %v", err)
	}
	got := string(data)
	for _, want := range []string{"http-client.private.env.json", ".env", ".apic/"} {
		if !strings.Contains(got, want) {
			t.Errorf("generated .gitignore is missing %q:\n%s", want, got)
		}
	}
}

// TestInitPointsAtTheSchema pins the modeline that gives editors completion
// and validation for apic.yaml, and that the file still parses as config.
func TestInitPointsAtTheSchema(t *testing.T) {
	dir := t.TempDir()
	app := New()
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr
	if code := app.Execute(context.Background(), []string{"init", dir, "--env", "qa"}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "apic.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# yaml-language-server: $schema=https://datagriff.github.io/api-caller/schemas/apic.schema.json\n") {
		t.Fatalf("apic.yaml should start with the schema modeline:\n%s", data)
	}
	p, err := project.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Config.Env != "qa" {
		t.Fatalf("env = %q, want qa", p.Config.Env)
	}
}

func TestImportDetectsPostmanCollections(t *testing.T) {
	dir := t.TempDir()
	col := filepath.Join(dir, "c.postman_collection.json")
	mustWrite(t, col, `{"info": {"name": "c", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"}, "item": [{"name": "ping", "request": "https://x/ping", "event": [{"listen": "test", "script": {"exec": ["pm.response.to.have.status(200);", "pm.foo();"]}}]}]}`)
	env := filepath.Join(dir, "dev.postman_environment.json")
	mustWrite(t, env, `{"name": "dev", "values": [{"key": "a", "value": "1", "enabled": true}], "_postman_variable_scope": "environment"}`)
	exec := func(args ...string) (string, int) {
		app := New()
		var stdout, stderr bytes.Buffer
		app.Stdout, app.Stderr = &stdout, &stderr
		code := app.Execute(context.Background(), args)
		return stdout.String() + stderr.String(), code
	}
	out, code := exec("import", col, "-o", filepath.Join(dir, "out"), "--postman-env", env)
	if code != 0 || !strings.Contains(out, "wrote "+filepath.Join(dir, "out", "c.http")) || !strings.Contains(out, "note  ping: pm.foo();:") || !strings.Contains(out, "1 request(s) generated") {
		t.Fatalf("import:\n%s", out)
	}
	if got := mustReadFile(t, filepath.Join(dir, "out", "apic.yaml")); !strings.Contains(got, "env: dev\n") {
		t.Errorf("apic.yaml = %q", got)
	}
	out, code = exec("--json", "import", col, "-o", filepath.Join(dir, "out2"))
	if code != 0 || !strings.Contains(out, `"unsupported"`) || !strings.Contains(out, `"requests": 1`) {
		t.Fatalf("import --json:\n%s", out)
	}
	if out, code = exec("import", env, "-o", dir); code != 2 || !strings.Contains(out, "is a Postman environment") {
		t.Errorf("an environment file alone: %d %s", code, out)
	}
	spec := filepath.Join(dir, "openapi.yaml")
	mustWrite(t, spec, "openapi: 3.0.0\ninfo: {title: t, version: '1'}\npaths: {}\n")
	if out, code = exec("import", spec, "--postman-env", env, "-o", dir); code != 2 || !strings.Contains(out, "--postman-env goes with") {
		t.Errorf("postman env with OpenAPI: %d %s", code, out)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestImportCurlAppendsARequest(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "http-client.env.json"), `{"dev": {"baseUrl": "https://api.example.com"}}`)
	mustWrite(t, filepath.Join(dir, "todos.http"), "### existing\n# @name post-todos\nPOST {{baseUrl}}/todos\n")
	exec := func(stdin string, args ...string) (string, string, int) {
		app := New()
		var stdout, stderr bytes.Buffer
		app.Stdout, app.Stderr, app.Stdin = &stdout, &stderr, strings.NewReader(stdin)
		code := app.Execute(context.Background(), append([]string{"-C", dir, "--env", "dev"}, args...))
		return stdout.String(), stderr.String(), code
	}
	cmd := `curl -X POST https://api.example.com/todos -H "Content-Type: application/json" -d '{"title":"x"}' --bogus`
	out, errOut, code := exec("", "import", "--curl", cmd, "--into", "todos.http")
	if code != 0 || out != "added post-todos-2 to todos.http\n" || !strings.Contains(errOut, "note: unknown flag --bogus") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	got := mustReadFile(t, filepath.Join(dir, "todos.http"))
	want := "### existing\n# @name post-todos\nPOST {{baseUrl}}/todos\n\n### POST /todos\n# @name post-todos-2\n# @assert status == 200\nPOST {{baseUrl}}/todos\nContent-Type: application/json\n\n{\"title\":\"x\"}\n"
	if got != want {
		t.Errorf("file:\n%s\nwant:\n%s", got, want)
	}
	// Printed without --into, read from stdin, named explicitly, as JSON.
	out, _, code = exec("curl https://api.example.com/health", "--json", "import", "--curl", "-", "--name", "ping")
	if code != 0 || !strings.Contains(out, `"name": "ping"`) || !strings.Contains(out, `GET {{baseUrl}}/health`) || !strings.Contains(out, `"warnings": []`) {
		t.Fatalf("stdin + json: code=%d out=%s", code, out)
	}
	if out, _, code = exec("", "import", "--curl", "curl -H 'a: b'"); code != 2 || out != "" {
		t.Errorf("no URL: code=%d out=%q", code, out)
	}
	if _, errOut, code = exec("", "import", "openapi.yaml", "--into", "x.http"); code != 2 || !strings.Contains(errOut, "--into and --name go with --curl") {
		t.Errorf("--into without --curl: code=%d err=%q", code, errOut)
	}
}
