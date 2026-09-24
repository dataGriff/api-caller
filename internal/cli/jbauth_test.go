package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// apic env lists the Security.Auth configurations with secrets masked, and
// validate reports one it cannot use and fields it ignores.
func TestEnvAndValidateShowSecurityAuth(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "api.http"), "GET https://x/me\nAuthorization: Bearer {{$auth.token(\"api\")}}\n")
	mustWrite(t, filepath.Join(dir, "http-client.env.json"), `{"dev": {"Security": {"Auth": {
  "api": {"Type": "OAuth2", "Grant Type": "Client Credentials", "Token URL": "https://idp/token", "Client ID": "cli", "Revoke URL": "r"},
  "old": {"Type": "OAuth2", "Grant Type": "Implicit", "Auth URL": "https://idp/auth", "Client ID": "cli"}}}}}`)
	mustWrite(t, filepath.Join(dir, "http-client.private.env.json"), `{"dev": {"Security": {"Auth": {"api": {"Client Secret": "s3cret"}}}}}`)
	code, out, errb := execute(t, "env", "-C", dir, "--env", "dev", "--json")
	if code != 0 || strings.Contains(out, "s3cret") || !strings.Contains(out, `"name": "api"`) || !strings.Contains(out, "clientSecret=***") || !strings.Contains(out, "Implicit grant is not supported") {
		t.Fatalf("env --json: code=%d out=%s err=%s", code, out, errb)
	}
	code, out, _ = execute(t, "env", "-C", dir, "--env", "dev", "--no-color")
	if code != 0 || strings.Contains(out, "s3cret") || !strings.Contains(out, "api oauth2 clientId=cli clientSecret=***") {
		t.Fatalf("env: code=%d out=%s", code, out)
	}
	code, out, _ = execute(t, "validate", "-C", dir, "--no-color")
	if code != 2 || !strings.Contains(out, `Security.Auth "old": the Implicit grant is not supported`) || !strings.Contains(out, `"Revoke URL" is not used by apic`) || !strings.Contains(out, "(bad-auth-config)") {
		t.Fatalf("validate: code=%d out=%s", code, out)
	}
}
