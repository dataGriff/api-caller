// Package postman turns a Postman collection (v2.1, or v2.0 where the
// shapes coincide) and its environments into .http files and env files:
// folders become files, requests become named requests, variables become
// http-client.env.json, and the trivial forms of pm.test scripts become
// `# @assert` and `# @capture` lines.
//
// The model below is hand-written from the collection format, on purpose:
// it reads only what the generator needs and tolerates the two shapes
// (string or object) the format allows for several fields.
package postman

import (
	"encoding/json"
	"strings"
)

// Collection is a Postman collection file.
type Collection struct {
	Info     Info       `json:"info"`
	Item     []Item     `json:"item"`
	Variable []Variable `json:"variable"`
	Auth     *Auth      `json:"auth"`
	Event    []Event    `json:"event"`
}

// Info holds the collection's name and schema URL.
type Info struct {
	Name        string   `json:"name"`
	Schema      string   `json:"schema"`
	Description flexText `json:"description"`
}

// Item is a request, or a folder when Item is non-empty (or Request nil).
type Item struct {
	Name        string   `json:"name"`
	Description flexText `json:"description"`
	Item        []Item   `json:"item"`
	Request     *Request `json:"request"`
	Event       []Event  `json:"event"`
	Auth        *Auth    `json:"auth"`
}

// IsFolder reports whether the item groups other items.
func (i Item) IsFolder() bool { return i.Request == nil }

// Request is the request of an item.
type Request struct {
	Method      string   `json:"method"`
	Header      []Header `json:"header"`
	URL         URL      `json:"url"`
	Body        *Body    `json:"body"`
	Auth        *Auth    `json:"auth"`
	Description flexText `json:"description"`
}

// UnmarshalJSON accepts the short form, a bare URL string, for a request.
func (r *Request) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		*r = Request{Method: "GET", URL: URL{Raw: raw}}
		return nil
	}
	type plain Request
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*r = Request(p)
	return nil
}

// Header is one request header; Disabled ones are kept as comments.
type Header struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
}

// URL is the request URL: the raw text, plus the parsed parts Postman
// keeps alongside it. Only Raw, Query and Variable are read.
type URL struct {
	Raw      string     `json:"raw"`
	Query    []Query    `json:"query"`
	Variable []Variable `json:"variable"`
}

// UnmarshalJSON accepts a bare string for the URL.
func (u *URL) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		*u = URL{Raw: raw}
		return nil
	}
	type plain URL
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*u = URL(p)
	return nil
}

// Query is one query parameter of a URL object.
type Query struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
}

// Variable is a collection, URL (path) or environment variable.
type Variable struct {
	Key      string   `json:"key"`
	Value    flexText `json:"value"`
	Type     string   `json:"type"` // "secret" in environments marks a masked value
	Disabled bool     `json:"disabled"`
	Enabled  *bool    `json:"enabled"` // environment files use enabled instead of disabled
}

// Active reports whether the variable is in effect.
func (v Variable) Active() bool {
	if v.Enabled != nil {
		return *v.Enabled
	}
	return !v.Disabled
}

// Body is the request body in one of Postman's modes.
type Body struct {
	Mode       string       `json:"mode"` // raw, urlencoded, formdata, file, graphql
	Raw        string       `json:"raw"`
	Options    *BodyOptions `json:"options"`
	Urlencoded []FormField  `json:"urlencoded"`
	Formdata   []FormField  `json:"formdata"`
	File       *BodyFile    `json:"file"`
	Graphql    *GraphQL     `json:"graphql"`
	Disabled   bool         `json:"disabled"`
}

// BodyOptions carries the raw body's language, which decides the content
// type when no header says.
type BodyOptions struct {
	Raw struct {
		Language string `json:"language"`
	} `json:"raw"`
}

// FormField is one urlencoded or form-data field.
type FormField struct {
	Key         string   `json:"key"`
	Value       string   `json:"value"`
	Type        string   `json:"type"` // "text" or "file"
	Src         flexText `json:"src"`  // a path, or a list of paths, for a file field
	ContentType string   `json:"contentType"`
	Disabled    bool     `json:"disabled"`
}

// BodyFile is a whole-body file.
type BodyFile struct {
	Src string `json:"src"`
}

// GraphQL is a GraphQL body: the query and its variables as JSON text.
type GraphQL struct {
	Query     string `json:"query"`
	Variables string `json:"variables"`
}

// Auth is an auth block on the collection, a folder or a request.
type Auth struct {
	Type   string      `json:"type"`
	Bearer []AuthParam `json:"bearer"`
	Basic  []AuthParam `json:"basic"`
	Apikey []AuthParam `json:"apikey"`
	Awsv4  []AuthParam `json:"awsv4"`
	Oauth2 []AuthParam `json:"oauth2"`
	Digest []AuthParam `json:"digest"`
}

// AuthParam is one key/value of an auth block.
type AuthParam struct {
	Key   string   `json:"key"`
	Value flexText `json:"value"`
}

// param returns the value of a key in an auth block's parameters.
func param(ps []AuthParam, key string) string {
	for _, p := range ps {
		if p.Key == key {
			return string(p.Value)
		}
	}
	return ""
}

// Event is a script hook: listen is "prerequest" or "test".
type Event struct {
	Listen string `json:"listen"`
	Script Script `json:"script"`
}

// Script holds the lines of a script.
type Script struct {
	Exec flexLines `json:"exec"`
}

// flexText is a string that the format also allows as an object
// (`{"content": "..."}` for descriptions), a number, a boolean or a list
// (`src` of a file field). Anything else becomes its JSON text.
type flexText string

func (f *flexText) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*f = flexText(textOf(v))
	return nil
}

func textOf(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		b, _ := json.Marshal(t)
		return string(b)
	case map[string]any:
		if c, ok := t["content"].(string); ok {
			return c
		}
		if c, ok := t["src"].(string); ok {
			return c
		}
	case []any:
		if len(t) > 0 {
			return textOf(t[0])
		}
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// flexLines is a script body: a list of lines, or one string.
type flexLines []string

func (f *flexLines) UnmarshalJSON(data []byte) error {
	var lines []string
	if err := json.Unmarshal(data, &lines); err == nil {
		*f = lines
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err != nil {
		return err
	}
	*f = strings.Split(one, "\n")
	return nil
}

// Environment is a Postman environment export.
type Environment struct {
	Name   string     `json:"name"`
	Values []Variable `json:"values"`
	Scope  string     `json:"_postman_variable_scope"`
}

// looksLikeCollection reports whether JSON data is a Postman collection
// (any version), for the import command's dispatch.
func looksLikeCollection(data []byte) bool {
	var probe struct {
		Info *struct {
			Schema string `json:"schema"`
		} `json:"info"`
		Item  json.RawMessage `json:"item"`
		Order json.RawMessage `json:"order"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	if probe.Info != nil && strings.Contains(probe.Info.Schema, "getpostman.com") {
		return true
	}
	return probe.Item != nil || probe.Order != nil
}

// LooksLikeCollection reports whether a file is a Postman collection.
func LooksLikeCollection(data []byte) bool { return looksLikeCollection(data) }

// LooksLikeEnvironment reports whether a file is a Postman environment.
func LooksLikeEnvironment(data []byte) bool {
	var probe struct {
		Values json.RawMessage `json:"values"`
		Scope  string          `json:"_postman_variable_scope"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Scope == "environment" || (probe.Values != nil && probe.Scope == "")
}
