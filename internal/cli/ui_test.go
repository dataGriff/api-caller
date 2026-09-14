package cli

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/dataGriff/api-caller/internal/project"
)

func TestUIRefusesJSONAndNonTTY(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir+"/api.http", "GET http://example.com\n")
	run := func(args ...string) (int, string) {
		a := New()
		var out, errb bytes.Buffer
		a.Stdout, a.Stderr = &out, &errb
		return a.Execute(context.Background(), args), errb.String()
	}
	if code, stderr := run("ui", "--json", "-C", dir); code != 2 || !strings.Contains(stderr, "--json") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if code, stderr := run("ui", "-C", dir); code != 2 || !strings.Contains(stderr, "interactive terminal") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestStartDemo(t *testing.T) {
	root, stop, err := startDemo()
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 2 {
		t.Fatalf("demo project should have 2 files, got %d", len(p.Files))
	}
	env, err := os.ReadFile(root + "/http-client.env.json")
	if err != nil {
		t.Fatal(err)
	}
	url := strings.TrimSpace(strings.Split(strings.Split(string(env), "\"baseUrl\": \"")[1], "\"")[0])
	resp, err := http.Get(url + "/todos")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("demo API should answer, got %d", resp.StatusCode)
	}
	stop()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("stop should remove the temp project")
	}
	if c, err := net.Dial("tcp", strings.TrimPrefix(url, "http://")); err == nil {
		_ = c.Close()
		t.Fatal("stop should close the listener")
	}
}

func TestInitScaffoldsAValidProject(t *testing.T) {
	dir := t.TempDir()
	a := New()
	var out, errb bytes.Buffer
	a.Stdout, a.Stderr = &out, &errb
	if code := a.Execute(context.Background(), []string{"init", dir, "--base-url", "http://localhost:1", "--env", "test"}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	for _, f := range []string{"apic.yaml", "http-client.env.json", "http-client.private.env.json", "api.http", "features/smoke.feature", ".gitignore"} {
		if _, err := os.Stat(dir + "/" + f); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
	}
	a = New()
	out.Reset()
	a.Stdout, a.Stderr = &out, &errb
	if code := a.Execute(context.Background(), []string{"validate", "-C", dir, "--json"}); code != 0 || !strings.Contains(out.String(), `"ok": true`) {
		t.Fatalf("scaffold should validate: code=%d out=%s", code, out.String())
	}
	out.Reset()
	a = New()
	a.Stdout, a.Stderr = &out, &errb
	if code := a.Execute(context.Background(), []string{"init", dir, "--json"}); code != 0 || !strings.Contains(out.String(), `"skipped"`) {
		t.Fatalf("second init should skip existing files: code=%d out=%s", code, out.String())
	}
	if code := New().Execute(context.Background(), []string{"init", dir, "--env", ""}); code != 2 {
		t.Fatalf("empty env should be a usage error, got %d", code)
	}
}

func TestVersionJSONAndListFilter(t *testing.T) {
	a := New()
	var out, errb bytes.Buffer
	a.Stdout, a.Stderr = &out, &errb
	if code := a.Execute(context.Background(), []string{"version", "--json"}); code != 0 || !strings.Contains(out.String(), `"version"`) || !strings.Contains(out.String(), `"os"`) {
		t.Fatalf("code=%d out=%s", code, out.String())
	}
	dir := t.TempDir()
	mustWrite(t, dir+"/a.http", "### One\n# @name alpha\nGET http://example.com/a\n\n### Two\n# @name beta\nGET http://example.com/b\n")
	out.Reset()
	a = New()
	a.Stdout, a.Stderr = &out, &errb
	if code := a.Execute(context.Background(), []string{"list", "bet", "-C", dir}); code != 0 || strings.Contains(out.String(), "alpha") || !strings.Contains(out.String(), "beta") {
		t.Fatalf("code=%d out=%s", code, out.String())
	}
	out.Reset()
	a = New()
	a.Stdout, a.Stderr = &out, &errb
	if code := a.Execute(context.Background(), []string{"list", "zzz", "-C", dir, "--json"}); code != 0 || !strings.Contains(out.String(), `"requests": []`) {
		t.Fatalf("code=%d out=%s", code, out.String())
	}
	out.Reset()
	a = New()
	a.Stdout, a.Stderr = &out, &errb
	if code := a.Execute(context.Background(), []string{"curl", "alpha", "-C", dir, "--json"}); code != 0 || !strings.Contains(out.String(), `"command": "curl`) {
		t.Fatalf("code=%d out=%s", code, out.String())
	}
}
