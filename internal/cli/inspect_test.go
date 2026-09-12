package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
