// Package template substitutes `{{expr}}` placeholders.
package template

import (
	"fmt"
	"regexp"
	"strings"
)

var rePlaceholder = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// Resolver returns the value for an expression such as `baseUrl`,
// `$uuid`, `$processEnv HOME` or `login.response.body.$.id`.
// ok=false means the expression is unknown; err reports a malformed one.
type Resolver func(expr string) (value string, ok bool, err error)

// MissingError lists expressions that could not be resolved.
type MissingError struct {
	Exprs []string
}

func (e *MissingError) Error() string {
	return "missing variables: " + strings.Join(e.Exprs, ", ")
}

// Exprs returns the distinct placeholder expressions in s, in order.
func Exprs(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range rePlaceholder.FindAllStringSubmatch(s, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Render substitutes every placeholder in s. Unknown expressions are left in
// place and reported together in a *MissingError; any other resolver error
// stops rendering immediately.
func Render(s string, resolve Resolver) (string, error) {
	var missing []string
	var firstErr error
	out := rePlaceholder.ReplaceAllStringFunc(s, func(m string) string {
		if firstErr != nil {
			return m
		}
		expr := strings.TrimSpace(m[2 : len(m)-2])
		v, ok, err := resolve(expr)
		if err != nil {
			firstErr = fmt.Errorf("{{%s}}: %w", expr, err)
			return m
		}
		if !ok {
			missing = append(missing, expr)
			return m
		}
		return v
	})
	if firstErr != nil {
		return out, firstErr
	}
	if len(missing) > 0 {
		return out, &MissingError{Exprs: dedupe(missing)}
	}
	return out, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
