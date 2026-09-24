package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/dataGriff/api-caller/internal/session"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionJSONMasksCachedTokens(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "GET http://example.com\n")
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	mustWrite(t, filepath.Join(dir, ".apic", "session.json"), "{\n  \"envs\": {\n    \"dev\": {\n      \"$oauth2:test\": \"{\\\"access_token\\\":\\\"secret-token\\\",\\\"refresh_token\\\":\\\"secret-refresh\\\",\\\"expires_at\\\":\\\""+expires+"\\\"}\",\n      \"$meta\": \"value-with-dollar-prefix\",\n      \"plain\": \"value\"\n    }\n  }\n}\n")

	app := New()
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr
	if code := app.Execute(context.Background(), []string{"--json", "-C", dir, "session"}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}

	var got map[string]map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["dev"]["plain"] != "value" {
		t.Fatalf("plain value missing: %+v", got)
	}
	if got["dev"]["$meta"] != "value-with-dollar-prefix" {
		t.Fatalf("non-auth $ key should not be rewritten: %+v", got)
	}
	if strings.Contains(got["dev"]["$oauth2:test"], "secret-token") || strings.Contains(got["dev"]["$oauth2:test"], "secret-refresh") {
		t.Fatalf("cached token leaked: %q", got["dev"]["$oauth2:test"])
	}
	if !strings.Contains(got["dev"]["$oauth2:test"], "token") {
		t.Fatalf("cached token metadata missing: %q", got["dev"]["$oauth2:test"])
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionListsAndClearsCookies(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "GET http://example.com\n")
	mustWrite(t, filepath.Join(dir, ".apic", "session.json"), `{"envs": {"dev": {"token": "t-1"}}}`)
	mustWrite(t, filepath.Join(dir, "http-client.env.json"), `{"dev": {}, "staging": {}}`)
	jar, err := session.OpenJar(dir)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("https://api.example.com/auth/login")
	jar.HTTP("dev").SetCookies(u, []*http.Cookie{{Name: "sid", Value: "secret-cookie", Path: "/", HttpOnly: true}})
	jar.HTTP("staging").SetCookies(u, []*http.Cookie{{Name: "sid", Value: "other", Path: "/"}})
	if err := jar.Save(); err != nil {
		t.Fatal(err)
	}
	exec := func(args ...string) (string, int) {
		app := New()
		var stdout, stderr bytes.Buffer
		app.Stdout, app.Stderr = &stdout, &stderr
		code := app.Execute(context.Background(), append([]string{"-C", dir}, args...))
		return stdout.String() + stderr.String(), code
	}
	out, code := exec("session")
	if code != 0 || !strings.Contains(out, "token = t-1") || !strings.Contains(out, "cookie sid = ***") || !strings.Contains(out, "api.example.com/") || !strings.Contains(out, "until cleared") || !strings.Contains(out, "staging") {
		t.Fatalf("session:\n%s", out)
	}
	if strings.Contains(out, "secret-cookie") {
		t.Fatal("cookie value printed")
	}
	out, code = exec("session", "cookies", "--json")
	var listed map[string][]map[string]any
	if err := json.Unmarshal([]byte(out), &listed); code != 0 || err != nil || strings.Contains(out, "secret-cookie") {
		t.Fatalf("session cookies --json: %v\n%s", err, out)
	}
	if c := listed["dev"]; len(c) != 1 || c[0]["name"] != "sid" || c[0]["domain"] != "api.example.com" || c[0]["path"] != "/" || c[0]["http_only"] != true || c[0]["value"] != nil {
		t.Fatalf("dev cookies = %v", c)
	}
	// clear takes the current environment's cookies with its captures.
	if out, code = exec("session", "clear", "--env", "dev"); code != 0 {
		t.Fatalf("clear: %s", out)
	}
	out, _ = exec("session")
	if strings.Contains(out, "token") || !strings.Contains(out, "staging") {
		t.Fatalf("after clear dev:\n%s", out)
	}
	if out, code = exec("session", "clear", "--all"); code != 0 {
		t.Fatalf("clear --all: %s", out)
	}
	if out, _ = exec("session", "cookies"); !strings.Contains(out, "no cookies stored") {
		t.Fatalf("after clear --all:\n%s", out)
	}
}
