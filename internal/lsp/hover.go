package lsp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dataGriff/api-caller/internal/runner"
)

var phRe = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// hover describes the `{{placeholder}}` under the cursor: its value
// (masked when secret) and source, or for a missing one the request that
// captures it, as `apic describe` reports them for the request the cursor
// is in; outside a request, as `apic env` does.
func (s *server) hover(p textDocumentPositionParams) (any, error) {
	path, ok := uriToPath(p.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	text := s.text(path)
	ls := lines(text)
	if p.Position.Line < 0 || p.Position.Line >= len(ls) {
		return nil, nil
	}
	line := ls[p.Position.Line]
	col := s.units.toByte(line, p.Position.Character)
	var inner string
	start, end := -1, -1
	for _, m := range phRe.FindAllStringSubmatchIndex(line, -1) {
		if col >= m[0] && col <= m[1] {
			inner, start, end = line[m[2]:m[3]], m[0], m[1]
			break
		}
	}
	if start < 0 {
		return nil, nil
	}
	st := s.projectFor(path)
	var vars []runner.VarInfo
	inRequest := false
	if r, err := s.runnerFor(st); err == nil {
		if req := requestAt(fileOf(st, path), text, p.Position.Line); req != nil {
			vars, inRequest = r.Describe(req).Variables, true
		} else {
			vars = r.EnvVars()
		}
	}
	md := hoverText(inner, vars, inRequest)
	if md == "" {
		return nil, nil
	}
	return hover{
		Contents: markupContent{Kind: "markdown", Value: md},
		Range: &lspRange{
			Start: position{Line: p.Position.Line, Character: s.units.toClient(line, start)},
			End:   position{Line: p.Position.Line, Character: s.units.toClient(line, end)},
		},
	}, nil
}

var refHoverRe = regexp.MustCompile(`^([\w-]+)\.response\.(body|headers)(.*)$`)

// hoverText is the Markdown for a placeholder's inner text, or "" when
// there is nothing to say. vars are the request's variables (inRequest)
// or the environment's.
func hoverText(inner string, vars []runner.VarInfo, inRequest bool) string {
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return ""
	}
	name := fields[0]
	if strings.HasPrefix(name, "$") {
		base, _, _ := strings.Cut(name, "(")
		for _, b := range builtins {
			if b.name == base || (b.name == "$env" && strings.HasPrefix(name, "$env.")) {
				return fmt.Sprintf("**%s** · built-in\n\n%s", name, b.doc)
			}
		}
		return fmt.Sprintf("**%s** · unknown built-in", name)
	}
	if m := refHoverRe.FindStringSubmatch(name); m != nil {
		what := "body"
		if m[2] == "headers" {
			what = "header"
		}
		return fmt.Sprintf("**%s** · response reference\n\nThe %s of `%s` from earlier in the same run.", name, what, m[1])
	}
	const unset = "No source defines it. Add it to an env file, `.env` or `--var`."
	for _, v := range vars {
		if v.Name != name {
			continue
		}
		if v.Missing {
			switch {
			case v.CapturedBy != "" && v.RefRuns:
				return fmt.Sprintf("**%s** · not set\n\nCaptured by `%s`, which `# @ref` runs first.", name, v.CapturedBy)
			case v.CapturedBy != "":
				return fmt.Sprintf("**%s** · not set\n\nCaptured by `%s`: run it first, or add `# @ref %s`.", name, v.CapturedBy, v.CapturedBy)
			}
			return fmt.Sprintf("**%s** · not set\n\n%s", name, unset)
		}
		value, note := v.Value, ""
		if v.Secret {
			value, note = runner.Masked, " (secret, masked)"
		}
		return fmt.Sprintf("**%s** = `%s`\n\nfrom %s%s", name, value, v.Source, note)
	}
	if inRequest {
		return fmt.Sprintf("**%s** · not set\n\n%s", name, unset)
	}
	return ""
}
