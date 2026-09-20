package httpfile

import (
	"bytes"
	"encoding/json"
	"net/textproto"
	"regexp"
	"sort"
	"strings"
)

// Format rewrites .http content in its canonical form. It works on the
// lines rather than the AST so that everything the parser skips
// (comments, unknown directives, editor script blocks) is kept verbatim:
//
//   - one blank line between blocks, `### Title` on its own line;
//   - directives in a fixed order (name, description, step, auth, ref,
//     forceRef, retry, timeout, no-redirect, no-session, no-cookies,
//     assert, capture, then unknown ones as written), each `# @key value`;
//     comments keep their place among the directives;
//   - `@name = value` file variables with single spaces;
//   - the request line with single spaces, query continuations indented
//     four spaces, header names in canonical case;
//   - a JSON body pretty-printed with two spaces when it parses and holds
//     no {{placeholders}}; every other body as written;
//   - trailing whitespace removed, one final newline.
//
// Formatting is idempotent: Format(Format(s)) == Format(s).
func Format(src string) string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	var out []string
	var block []string
	var title string
	implicit := true // the text before the first separator has no title line
	flush := func() {
		formatted := formatBlock(title, block, implicit)
		if len(formatted) > 0 {
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, formatted...)
		}
		implicit = false
	}
	for _, line := range lines {
		if m := reSeparator.FindStringSubmatch(line); m != nil {
			flush()
			title, block = strings.TrimSpace(m[1]), nil
			continue
		}
		block = append(block, line)
	}
	flush()
	// One final newline, none before it.
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// directiveRank orders the known directives; unknown ones come after,
// in the order they were written.
var directiveRank = map[string]int{
	"name": 0, "description": 1, "step": 2, "auth": 3, "ref": 4, "forceRef": 5, "retry": 6,
	"timeout": 7, "no-redirect": 8, "no-session": 9, "no-cookies": 10, "assert": 11, "capture": 12,
}

var reBodyFile = regexp.MustCompile(`^<@?\s+`)

// formatBlock formats one `###` block; implicit is the text before the
// first separator, which has no title line.
func formatBlock(title string, lines []string, implicit bool) []string {
	var out []string
	if !implicit {
		if title != "" {
			out = append(out, "### "+title)
		} else {
			out = append(out, "###")
		}
	}
	i := 0
	// Preamble: comments, directives and file variables up to the request
	// line, with blank lines dropped.
	type item struct {
		text      string
		directive bool
		rank      int
		order     int
	}
	var preamble []item
	for ; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		if m := reFileVar.FindStringSubmatch(t); m != nil {
			preamble = append(preamble, item{text: "@" + m[1] + " = " + strings.TrimSpace(m[2])})
			continue
		}
		if m := reComment.FindStringSubmatch(t); m != nil {
			text := strings.TrimSpace(m[1])
			if d := reDirective.FindStringSubmatch(text); d != nil {
				key, value := d[1], strings.TrimSpace(d[2])
				line := "# @" + key
				if value != "" {
					line += " " + value
				}
				rank, known := directiveRank[key]
				if !known {
					rank = len(directiveRank)
				}
				preamble = append(preamble, item{text: line, directive: true, rank: rank, order: len(preamble)})
				continue
			}
			preamble = append(preamble, item{text: strings.TrimRight(lines[i], " \t")})
			continue
		}
		break
	}
	// Directives are sorted among themselves and put back into the slots
	// they occupied, so a comment stays next to what it was written by.
	var directives []item
	for _, it := range preamble {
		if it.directive {
			directives = append(directives, it)
		}
	}
	sort.SliceStable(directives, func(a, b int) bool { return directives[a].rank < directives[b].rank })
	next := 0
	for _, it := range preamble {
		if it.directive {
			out = append(out, directives[next].text)
			next++
			continue
		}
		out = append(out, it.text)
	}
	if i >= len(lines) {
		return trimTrailingBlank(out)
	}

	// Request line.
	reqLine := strings.TrimSpace(lines[i])
	if m := reRequestLine.FindStringSubmatch(reqLine); m != nil {
		reqLine = m[1] + " " + m[2]
		if m[3] != "" {
			reqLine += " " + m[3]
		}
	} else {
		reqLine = strings.Join(strings.Fields(reqLine), " ")
	}
	out = append(out, reqLine)
	i++
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if (strings.HasPrefix(t, "?") || strings.HasPrefix(t, "&")) && lines[i] != t {
			out = append(out, "    "+t)
			i++
			continue
		}
		break
	}

	// Headers until the first blank line; comments among them stay.
	var headers []string
	sawBlank := false
	for ; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			sawBlank = true
			i++
			break
		}
		if reComment.MatchString(t) {
			headers = append(headers, t)
			continue
		}
		if m := reHeader.FindStringSubmatch(t); m != nil {
			headers = append(headers, textproto.CanonicalMIMEHeaderKey(m[1])+": "+strings.TrimSpace(m[2]))
			continue
		}
		headers = append(headers, strings.TrimRight(lines[i], " \t")) // the parser will report it
	}
	out = append(out, headers...)
	if !sawBlank {
		return trimTrailingBlank(out)
	}

	// Body: everything else, as written, trailing whitespace and trailing
	// blank lines removed; a plain JSON document is pretty-printed.
	body := lines[i:]
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	for len(body) > 0 && strings.TrimSpace(body[0]) == "" {
		body = body[1:]
	}
	if len(body) == 0 {
		return trimTrailingBlank(out)
	}
	out = append(out, "")
	joined := strings.Join(body, "\n")
	if pretty, ok := prettyJSON(joined, headers); ok {
		return append(out, strings.Split(pretty, "\n")...)
	}
	for _, l := range body {
		out = append(out, strings.TrimRight(l, " \t"))
	}
	return out
}

// prettyJSON re-indents a body that is one JSON document without
// placeholders or handler blocks, under a JSON content type or none.
func prettyJSON(body string, headers []string) (string, bool) {
	t := strings.TrimSpace(body)
	if t == "" || reBodyFile.MatchString(t) || strings.Contains(t, "{{") || strings.Contains(t, "{%") {
		return "", false
	}
	if t[0] != '{' && t[0] != '[' {
		return "", false
	}
	for _, h := range headers {
		if k, v, ok := strings.Cut(h, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Type") {
			ct := strings.ToLower(strings.TrimSpace(v))
			if !strings.Contains(ct, "json") {
				return "", false
			}
		}
	}
	if !json.Valid([]byte(t)) {
		return "", false
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(t), "", "  "); err != nil {
		return "", false
	}
	return buf.String(), true
}

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
