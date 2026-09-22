package httpfile

import (
	"encoding/json"
	"errors"
	"strings"
)

// GraphQL requests, written the way both editors write them: the request
// line says `GRAPHQL` (JetBrains) or a header says `X-REQUEST-TYPE:
// GraphQL` (REST Client), the body is the query, and an optional JSON
// object after a blank line is the variables. apic sends them as a POST
// with a `{"query", "variables"}` JSON body, which is what a GraphQL
// server reads; the runner does that, this file only recognises the shape.

// GraphQLHeader is the REST Client header that marks a GraphQL request.
// It is never sent on the wire.
const GraphQLHeader = "X-REQUEST-TYPE"

// IsGraphQL reports whether the request is written as a GraphQL query:
// the `GRAPHQL` method, or an `X-REQUEST-TYPE: GraphQL` header.
func (r *Request) IsGraphQL() bool {
	if r.Method == "GRAPHQL" {
		return true
	}
	v, ok := r.Header(GraphQLHeader)
	return ok && strings.EqualFold(strings.TrimSpace(v), "GraphQL")
}

// SplitGraphQL separates a GraphQL body into the query and the variables:
// the variables are the JSON object after the last blank line, when what
// follows that line starts with `{`. The variables are "" when there are
// none. Both halves keep their placeholders.
func SplitGraphQL(body string) (query, variables string) {
	lines := strings.Split(body, "\n")
	for i := len(lines) - 1; i > 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			continue
		}
		rest := strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		if strings.HasPrefix(rest, "{") {
			return strings.TrimSpace(strings.Join(lines[:i], "\n")), rest
		}
		break
	}
	return strings.TrimSpace(body), ""
}

// GraphQL returns the query and the variables of a GraphQL request as
// written, and an error when the request has no query or the variables
// hold no placeholders and are not a JSON object.
func (r *Request) GraphQL() (query, variables string, err error) {
	if r.BodyFile != "" {
		return "", "", nil // the file is the query; the runner reads it
	}
	query, variables = SplitGraphQL(r.Body)
	if query == "" {
		return "", "", errors.New("a GraphQL request needs a query in its body")
	}
	if variables != "" && !strings.Contains(variables, "{{") && !json.Valid([]byte(variables)) {
		return "", "", errors.New("the GraphQL variables after the blank line are not a JSON object")
	}
	return query, variables, nil
}

// GraphQLEnvelope is the body sent for a GraphQL request: the query as a
// JSON string and the variables object, when there is one. The variables
// must be a JSON object by then.
func GraphQLEnvelope(query, variables string) (string, error) {
	env := struct {
		Query     string          `json:"query"`
		Variables json.RawMessage `json:"variables,omitempty"`
	}{Query: query}
	if v := strings.TrimSpace(variables); v != "" {
		if !json.Valid([]byte(v)) || !strings.HasPrefix(v, "{") {
			return "", errors.New("the GraphQL variables are not a JSON object")
		}
		env.Variables = json.RawMessage(v)
	}
	out, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
