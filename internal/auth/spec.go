// Package auth applies authentication to outgoing requests, driven by the
// `# @auth <type> [args]` directive or the `auth.default` setting in
// apic.yaml. Supported types: none, bearer, basic, aws, oauth2, exec.
package auth

import (
	"fmt"
	"sort"
	"strings"
)

// Spec is a parsed `# @auth` value. Args holds positional arguments and
// Options holds key=value arguments; both may still contain {{placeholders}}
// until the runner renders them.
type Spec struct {
	Type    string
	Args    []string
	Options map[string]string
	Raw     string
}

// Types lists the supported auth types with a one-line description.
var Types = map[string]string{
	"none":   "send no credentials (overrides a project default)",
	"bearer": "bearer <token>: Authorization: Bearer <token>",
	"basic":  "basic <user> <password>: HTTP basic auth",
	"aws":    "aws [service=execute-api] [region=..] [profile=..]: AWS Signature V4 using the SDK credential chain",
	"oauth2": "oauth2 tokenUrl=.. clientId=.. [clientSecret=..] [grant=client_credentials|password|device_code] [scope=..] [username=..] [password=..] [audience=..] [deviceUrl=..] [clientAuth=body|basic]",
	"exec":   "exec <command> [args..] [header=Authorization] [prefix=Bearer] [ttl=10m]: use a command's stdout as the token (needs auth.allowExec in apic.yaml)",
}

// Parse splits an auth spec such as `basic {{user}} {{password}}` or
// `aws service=execute-api region=eu-west-2`.
func Parse(raw string) (*Spec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("@auth needs a type: one of %s", strings.Join(typeNames(), ", "))
	}
	fields, err := split(raw)
	if err != nil {
		return nil, fmt.Errorf("@auth %q: %w", raw, err)
	}
	s := &Spec{Type: strings.ToLower(fields[0]), Options: map[string]string{}, Raw: raw}
	if _, ok := Types[s.Type]; !ok {
		return nil, fmt.Errorf("@auth: unknown type %q (one of %s)", fields[0], strings.Join(typeNames(), ", "))
	}
	for _, f := range fields[1:] {
		// exec keeps its command words positional even when they contain '='.
		if k, v, ok := strings.Cut(f, "="); ok && (s.Type != "exec" || isExecOption(k)) && isIdent(k) {
			s.Options[k] = v
			continue
		}
		s.Args = append(s.Args, f)
	}
	return s, s.check()
}

func (s *Spec) check() error {
	switch s.Type {
	case "none":
		if len(s.Args) > 0 || len(s.Options) > 0 {
			return fmt.Errorf("@auth none takes no arguments")
		}
	case "bearer":
		if len(s.Args) != 1 {
			return fmt.Errorf("@auth bearer needs exactly one argument: the token")
		}
	case "basic":
		if len(s.Args) != 2 {
			return fmt.Errorf("@auth basic needs two arguments: user and password")
		}
	case "aws":
		if len(s.Args) > 0 {
			return fmt.Errorf("@auth aws takes only key=value options (service, region, profile)")
		}
		for k := range s.Options {
			if k != "service" && k != "region" && k != "profile" {
				return fmt.Errorf("@auth aws: unknown option %q", k)
			}
		}
	case "oauth2":
		if len(s.Args) > 0 {
			return fmt.Errorf("@auth oauth2 takes only key=value options")
		}
		if s.Options["tokenUrl"] == "" {
			return fmt.Errorf("@auth oauth2 needs tokenUrl=")
		}
		if s.Options["clientId"] == "" {
			return fmt.Errorf("@auth oauth2 needs clientId=")
		}
		switch g := s.grant(); g {
		case "client_credentials":
		case "password":
			if s.Options["username"] == "" || s.Options["password"] == "" {
				return fmt.Errorf("@auth oauth2 grant=password needs username= and password=")
			}
		case "device_code":
			if s.Options["deviceUrl"] == "" {
				return fmt.Errorf("@auth oauth2 grant=device_code needs deviceUrl=")
			}
		default:
			return fmt.Errorf("@auth oauth2: unknown grant %q (client_credentials, password or device_code)", g)
		}
		for k := range s.Options {
			switch k {
			case "tokenUrl", "clientId", "clientSecret", "grant", "scope", "username", "password", "audience", "deviceUrl", "clientAuth":
			default:
				return fmt.Errorf("@auth oauth2: unknown option %q", k)
			}
		}
		if ca := s.Options["clientAuth"]; ca != "" && ca != "body" && ca != "basic" {
			return fmt.Errorf("@auth oauth2: clientAuth must be body or basic")
		}
	case "exec":
		if len(s.Args) == 0 {
			return fmt.Errorf("@auth exec needs a command")
		}
	}
	return nil
}

func (s *Spec) grant() string {
	if g := s.Options["grant"]; g != "" {
		return g
	}
	return "client_credentials"
}

func isExecOption(k string) bool {
	return k == "header" || k == "prefix" || k == "ttl"
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return true
}

func typeNames() []string {
	names := make([]string, 0, len(Types))
	for n := range Types {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// split tokenises on whitespace, honouring single and double quotes so that
// values with spaces can be written as key="a b".
func split(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inWord := false
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			quote = c
			inWord = true
		case c == ' ' || c == '\t':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out, nil
}

// Render returns a copy of the spec with fn applied to every argument and
// option value (used to substitute {{placeholders}}).
func (s *Spec) Render(fn func(string) (string, error)) (*Spec, error) {
	out := &Spec{Type: s.Type, Options: map[string]string{}, Raw: s.Raw}
	for _, a := range s.Args {
		v, err := fn(a)
		if err != nil {
			return nil, err
		}
		out.Args = append(out.Args, v)
	}
	for k, a := range s.Options {
		v, err := fn(a)
		if err != nil {
			return nil, err
		}
		out.Options[k] = v
	}
	return out, nil
}

// Texts returns every string in the spec that may contain placeholders.
func (s *Spec) Texts() []string {
	out := append([]string(nil), s.Args...)
	for _, v := range s.Options {
		out = append(out, v)
	}
	return out
}
