// Package snippet renders a resolved request as code that sends the same
// thing without apic: a curl command, an HTTPie command, PowerShell's
// Invoke-RestMethod, Python requests, JavaScript fetch or Go net/http.
//
// Every language follows curl's rules. Without redact the snippet runs as
// printed, credentials included. With redact the header values, body and
// query values are masked and credentials come from the environment:
// TOKEN for a bearer or OAuth2 token, APIC_USER and APIC_PASSWORD for
// basic and digest, APIC_API_KEY for an API key. What a language cannot
// do on its own (AWS SigV4 signing, digest in fetch) is said in a comment.
package snippet

import (
	"fmt"
	"strings"

	"github.com/dataGriff/api-caller/internal/auth"
	"github.com/dataGriff/api-caller/internal/httpfile"
	"github.com/dataGriff/api-caller/internal/runner"
)

// Languages are the snippet languages, in the order the UI cycles them.
var Languages = []string{"curl", "httpie", "powershell", "python", "js", "go"}

// Render returns the snippet for lang.
func Render(lang string, r *runner.Resolved, redact bool) (string, error) {
	m := model(r, redact)
	switch lang {
	case "curl":
		return Curl(r, redact), nil
	case "httpie":
		return httpie(m), nil
	case "powershell":
		return powershell(m), nil
	case "python":
		return python(m), nil
	case "js":
		return js(m), nil
	case "go":
		return goCode(m)
	}
	return "", fmt.Errorf("unknown language %q: one of %s", lang, strings.Join(Languages, ", "))
}

// request is what every renderer but curl's works from: the request as
// it goes out, with its credential described rather than applied.
type request struct {
	Method  string
	URL     string
	Headers []httpfile.Header // without Content-Type when Parts set it
	Body    string
	Parts   []runner.FormPart
	Redact  bool
	Cred    *credential
	TLS     *runner.TLSInfo
	Proxy   string // the proxy URL, "" for none
	HTTP    string // httpfile.HTTP1, httpfile.HTTP2 or ""
}

// secret is a credential value: written literally, or read from an
// environment variable.
type secret struct {
	Literal string
	Env     string
}

// credential is how a request authenticates, in terms each language can
// express.
type credential struct {
	Kind   string // "header", "query", "basic", "digest", "note"
	Name   string // header or query parameter name
	Prefix string // text before the value, e.g. "Bearer "
	Value  secret
	User   secret // basic and digest
	Pass   secret
	Note   string // why the language cannot do it, for "note" and alongside others
}

func model(r *runner.Resolved, redact bool) *request {
	m := &request{Method: r.Method, URL: r.DisplayURL(redact), Parts: r.Parts, Redact: redact, TLS: r.TLS, HTTP: r.HTTPVersion}
	headers := r.Headers
	if redact {
		headers = r.DisplayHeaders(true)
	}
	for _, h := range headers {
		if len(r.Parts) > 0 && strings.EqualFold(h.Name, "Content-Type") {
			continue // each language writes its own boundary
		}
		m.Headers = append(m.Headers, h)
	}
	if len(r.Parts) == 0 {
		m.Body = r.DisplayBody(redact)
	}
	if p := r.Proxy; p != nil && !p.Off {
		m.Proxy = p.URL
		if !redact && p.Raw() != nil {
			m.Proxy = p.Raw().String()
		}
	}
	m.Cred = credentialOf(r.AuthSpec, redact)
	return m
}

func lit(v string, redact bool, env string) secret {
	if redact {
		return secret{Env: env}
	}
	return secret{Literal: v}
}

func credentialOf(s *auth.Spec, redact bool) *credential {
	if s == nil {
		return nil
	}
	switch s.Type {
	case "bearer":
		return &credential{Kind: "header", Name: "Authorization", Prefix: "Bearer ", Value: lit(s.Args[0], redact, "TOKEN")}
	case "basic", "digest":
		return &credential{Kind: s.Type, User: lit(s.Args[0], redact, "APIC_USER"), Pass: lit(s.Args[1], redact, "APIC_PASSWORD")}
	case "apikey":
		header, query := s.APIKeyPlacement()
		c := &credential{Kind: "header", Name: header, Prefix: s.Options["prefix"], Value: lit(s.Args[0], redact, "APIC_API_KEY")}
		if query != "" {
			c.Kind, c.Name = "query", query
		}
		return c
	case "oauth2":
		return &credential{Kind: "header", Name: "Authorization", Prefix: "Bearer ", Value: secret{Env: "TOKEN"},
			Note: "TOKEN is an OAuth2 access token from " + s.Options["tokenUrl"]}
	case "exec":
		header := s.Options["header"]
		if header == "" {
			header = "Authorization"
		}
		prefix, ok := s.Options["prefix"]
		if !ok {
			prefix = "Bearer"
		}
		if prefix != "" {
			prefix += " "
		}
		return &credential{Kind: "header", Name: header, Prefix: prefix, Value: secret{Env: "TOKEN"},
			Note: "TOKEN is the output of: " + strings.Join(s.Args, " ")}
	case "aws":
		service, region := s.Options["service"], s.Options["region"]
		if service == "" {
			service = "execute-api"
		}
		if region == "" {
			region = "$AWS_REGION"
		}
		return &credential{Kind: "note", Note: fmt.Sprintf("sign this request with AWS Signature V4 (service %s, region %s); apic does it with the AWS credential chain", service, region)}
	}
	return nil
}

// notes lists what the snippet does not carry over, as comment lines.
func (m *request) notes(tls, proxy, digest bool) []string {
	var out []string
	if c := m.Cred; c != nil && c.Note != "" {
		out = append(out, c.Note)
	}
	if c := m.Cred; c != nil && c.Kind == "digest" && !digest {
		out = append(out, "the API uses HTTP digest auth, which this snippet does not answer; apic does")
	}
	if t := m.TLS; t != nil && !tls {
		out = append(out, "apic's TLS settings are not carried over: "+t.String())
	}
	if m.Proxy != "" && !proxy {
		out = append(out, "apic sends this through the proxy "+m.Proxy)
	}
	if m.HTTP == httpfile.HTTP2 {
		out = append(out, "the request line asks for HTTP/2, which this client does not require")
	}
	return out
}
