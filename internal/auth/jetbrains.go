package auth

import (
	"fmt"
	"sort"
	"strings"
)

// JetBrains is a JetBrains HTTP Client `Security.Auth` configuration mapped
// onto an oauth2 spec, so {{$auth.token("name")}} runs the same grant and
// shares the same token cache as `# @auth oauth2`.
type JetBrains struct {
	Spec *Spec
	// UseIDToken is the configuration's "Use ID Token": $auth.token then
	// gives the ID token rather than the access token.
	UseIDToken bool
	// Ignored names the fields apic does not act on; validate warns about
	// each.
	Ignored []string
}

// jetBrainsOptions maps each JetBrains field onto an oauth2 option.
var jetBrainsOptions = map[string]string{
	"token url":       "tokenUrl",
	"auth url":        "authUrl",
	"device auth url": "deviceUrl",
	"client id":       "clientId",
	"client secret":   "clientSecret",
	"scope":           "scope",
	"username":        "username",
	"password":        "password",
	"redirect url":    "redirectUrl",
}

// jetBrainsGrants are the JetBrains "Grant Type" values and apic's names.
var jetBrainsGrants = map[string]string{
	"client credentials":   "client_credentials",
	"password":             "password",
	"device authorization": "device_code",
	"authorization code":   "authorization_code",
}

// jetBrainsKnown are fields apic reads but that need no option: the type
// and grant, and behaviour apic has anyway (it always uses PKCE, and
// fetches a token when a request needs one).
var jetBrainsKnown = map[string]bool{"type": true, "grant type": true, "client credentials": true,
	"custom request parameters": true, "use id token": true, "pkce": true, "acquire automatically": true}

// FromJetBrains maps a Security.Auth entry. render substitutes the
// {{placeholders}} a string field may hold; the result is checked like a
// written `# @auth oauth2`. The Implicit grant is refused: it is deprecated
// by OAuth 2.1, and Authorization Code with PKCE replaces it.
func FromJetBrains(fields map[string]any, render func(string) (string, error)) (*JetBrains, error) {
	jb := &JetBrains{Spec: &Spec{Type: "oauth2", Options: map[string]string{}}}
	get := func(key string) (any, bool) {
		for k, v := range fields {
			if strings.EqualFold(k, key) {
				return v, true
			}
		}
		return nil, false
	}
	str := func(key string) (string, error) {
		v, ok := get(key)
		if !ok || v == nil {
			return "", nil
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("%q must be a string", key)
		}
		return render(s)
	}
	typ, err := str("Type")
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(typ, "OAuth2") {
		return nil, fmt.Errorf("the Type %q is not supported: apic reads OAuth2 configurations", typ)
	}
	grant, err := str("Grant Type")
	if err != nil {
		return nil, err
	}
	switch g := strings.ToLower(grant); {
	case g == "implicit":
		return nil, fmt.Errorf("the Implicit grant is not supported (OAuth 2.1 drops it); use Authorization Code, which apic runs with PKCE")
	case g == "":
		return nil, fmt.Errorf(`"Grant Type" is missing: Client Credentials, Password, Device Authorization or Authorization Code`)
	case jetBrainsGrants[g] == "":
		return nil, fmt.Errorf("the Grant Type %q is not supported: Client Credentials, Password, Device Authorization or Authorization Code", grant)
	default:
		jb.Spec.Options["grant"] = jetBrainsGrants[g]
	}
	for k := range fields {
		opt, ok := jetBrainsOptions[strings.ToLower(k)]
		if !ok {
			if !jetBrainsKnown[strings.ToLower(k)] {
				jb.Ignored = append(jb.Ignored, k)
			}
			continue
		}
		v, err := str(k)
		if err != nil {
			return nil, err
		}
		if v != "" {
			jb.Spec.Options[opt] = v
		}
	}
	switch ca, err := str("Client Credentials"); {
	case err != nil:
		return nil, err
	case strings.EqualFold(ca, "basic"):
		jb.Spec.Options["clientAuth"] = "basic"
	case ca == "", strings.EqualFold(ca, "in body"), strings.EqualFold(ca, "none"):
	default:
		return nil, fmt.Errorf(`"Client Credentials" %q: use basic or in body`, ca)
	}
	if v, ok := get("Use ID Token"); ok {
		b, isBool := v.(bool)
		if !isBool {
			return nil, fmt.Errorf(`"Use ID Token" must be true or false`)
		}
		jb.UseIDToken = b
	}
	if v, ok := get("Custom Request Parameters"); ok {
		params, isObj := v.(map[string]any)
		if !isObj {
			return nil, fmt.Errorf(`"Custom Request Parameters" must be an object`)
		}
		for name, p := range params {
			// A parameter is a value, or {"Value": ..., "Use": ...}.
			if obj, isObj := p.(map[string]any); isObj {
				p = obj["Value"]
			}
			s, isStr := p.(string)
			if !strings.EqualFold(name, "audience") || !isStr {
				jb.Ignored = append(jb.Ignored, "Custom Request Parameters."+name)
				continue
			}
			v, err := render(s)
			if err != nil {
				return nil, err
			}
			jb.Spec.Options["audience"] = v
		}
	}
	sort.Strings(jb.Ignored)
	jb.Spec.Raw = jb.Spec.describe()
	if err := jb.Spec.check(); err != nil {
		// Say it in the file's own words: "Token URL", not tokenUrl=.
		msg := strings.Replace(err.Error(), "@auth oauth2", "the configuration", 1)
		for field, opt := range jetBrainsOptions {
			msg = strings.ReplaceAll(msg, opt+"=", fmt.Sprintf("%q", jetBrainsField(field)))
		}
		return nil, fmt.Errorf("%s", strings.ReplaceAll(msg, "grant=", `"Grant Type" `))
	}
	return jb, nil
}

// describe renders an oauth2 spec as `oauth2 key=value ...` without its
// secrets, for describe and apic env.
func (s *Spec) describe() string {
	keys := make([]string, 0, len(s.Options))
	for k := range s.Options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{s.Type}
	for _, k := range keys {
		v := s.Options[k]
		if k == "clientSecret" || k == "password" {
			v = "***"
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, " ")
}

// jetBrainsField is a lower-cased field name in JetBrains' own spelling.
func jetBrainsField(lower string) string {
	words := strings.Fields(lower)
	for i, w := range words {
		switch w {
		case "url", "id":
			words[i] = strings.ToUpper(w)
		default:
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
