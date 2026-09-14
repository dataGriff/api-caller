package output

import "strings"

// HighlightJSON colours the keys, strings, numbers and literals of an
// already-indented, valid JSON document. It works line by line (JSON strings
// never span lines) and passes anything unexpected through untouched, so a
// document that is not JSON comes back unchanged.
func HighlightJSON(t Theme, s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = highlightLine(t, line)
	}
	return strings.Join(lines, "\n")
}

func highlightLine(t Theme, line string) string {
	var out strings.Builder
	i := 0
	for i < len(line) {
		c := line[i]
		switch {
		case c == '"':
			end := closingQuote(line, i)
			if end < 0 {
				out.WriteString(line[i:])
				return out.String()
			}
			tok := line[i : end+1]
			rest := strings.TrimLeft(line[end+1:], " ")
			if strings.HasPrefix(rest, ":") {
				out.WriteString(t.Key.Render(tok))
			} else {
				out.WriteString(t.Str.Render(tok))
			}
			i = end + 1
		case c == '-' || (c >= '0' && c <= '9'):
			j := i
			for j < len(line) && strings.IndexByte("0123456789eE+-.", line[j]) >= 0 {
				j++
			}
			out.WriteString(t.Num.Render(line[i:j]))
			i = j
		case c == 't' || c == 'f' || c == 'n':
			j := i
			for j < len(line) && line[j] >= 'a' && line[j] <= 'z' {
				j++
			}
			out.WriteString(t.Lit.Render(line[i:j]))
			i = j
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// closingQuote returns the index of the quote that closes the string opening
// at line[start], honouring backslash escapes, or -1.
func closingQuote(line string, start int) int {
	for i := start + 1; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}
