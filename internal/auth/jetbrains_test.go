package auth

import (
	"strings"
	"testing"
)

func identity(s string) (string, error) { return s, nil }

// Each JetBrains grant maps onto the oauth2 options `# @auth oauth2` takes.
func TestFromJetBrains(t *testing.T) {
	cases := []struct {
		fields map[string]any
		want   string
	}{
		{map[string]any{"Type": "OAuth2", "Grant Type": "Client Credentials", "Token URL": "https://idp/token", "Client ID": "{{clientId}}", "Client Secret": "{{secret}}", "Scope": "read write", "Client Credentials": "basic"},
			"oauth2 clientAuth=basic clientId={{clientId}} clientSecret=*** grant=client_credentials scope=read write tokenUrl=https://idp/token"},
		{map[string]any{"Type": "OAuth2", "Grant Type": "Password", "Token URL": "t", "Client ID": "c", "Username": "u", "Password": "p", "Client Credentials": "in body"},
			"oauth2 clientId=c grant=password password=*** tokenUrl=t username=u"},
		{map[string]any{"Type": "OAuth2", "Grant Type": "Device Authorization", "Token URL": "t", "Client ID": "c", "Device Auth URL": "d"},
			"oauth2 clientId=c deviceUrl=d grant=device_code tokenUrl=t"},
		{map[string]any{"type": "oauth2", "grant type": "authorization code", "token url": "t", "client id": "c", "Auth URL": "a", "Redirect URL": "http://localhost:9876/cb", "PKCE": true,
			"Custom Request Parameters": map[string]any{"audience": map[string]any{"Value": "api://x", "Use": "Everywhere"}}},
			"oauth2 audience=api://x authUrl=a clientId=c grant=authorization_code redirectUrl=http://localhost:9876/cb tokenUrl=t"},
	}
	for _, c := range cases {
		jb, err := FromJetBrains(c.fields, identity)
		if err != nil {
			t.Fatalf("%v: %v", c.fields, err)
		}
		if jb.Spec.Raw != c.want || len(jb.Ignored) != 0 {
			t.Errorf("got %q (ignored %v)\nwant %q", jb.Spec.Raw, jb.Ignored, c.want)
		}
	}
}

func TestFromJetBrainsRefusesAndReports(t *testing.T) {
	for fields, want := range map[*map[string]any]string{
		{"Type": "OAuth2", "Grant Type": "Implicit", "Auth URL": "a", "Client ID": "c"}: "Implicit grant is not supported",
		{"Type": "Basic"}:  `Type "Basic" is not supported`,
		{"Type": "OAuth2"}: `"Grant Type" is missing`,
		{"Type": "OAuth2", "Grant Type": "Client Credentials", "Client ID": "c"}:                                                                              `needs "Token URL"`,
		{"Type": "OAuth2", "Grant Type": "Password", "Token URL": "t", "Client ID": "c"}:                                                                      `needs "Username" and "Password"`,
		{"Type": "OAuth2", "Grant Type": "Authorization Code", "Token URL": "t", "Client ID": "c", "Auth URL": "a", "Redirect URL": "https://example.com/cb"}: "must be an http:// URL on this machine",
		{"Type": "OAuth2", "Grant Type": "Client Credentials", "Token URL": "t", "Client ID": "c", "Client Credentials": "sideways"}:                          `use basic or in body`,
		{"Type": "OAuth2", "Grant Type": "Client Credentials", "Token URL": 7, "Client ID": "c"}:                                                              `"Token URL" must be a string`,
	} {
		if _, err := FromJetBrains(*fields, identity); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: got %v, want %q", *fields, err, want)
		}
	}
	jb, err := FromJetBrains(map[string]any{"Type": "OAuth2", "Grant Type": "Client Credentials", "Token URL": "t", "Client ID": "c", "Use ID Token": true,
		"Revoke URL": "r", "Custom Request Parameters": map[string]any{"resource": "x"}}, identity)
	if err != nil || !jb.UseIDToken || strings.Join(jb.Ignored, ",") != "Custom Request Parameters.resource,Revoke URL" {
		t.Fatalf("%+v %v", jb, err)
	}
}
