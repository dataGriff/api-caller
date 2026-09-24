package postman

import (
	"regexp"
	"strings"
)

// translated is what a test script contributed to a request.
type translated struct {
	Asserts     []string
	Captures    []string
	Unsupported []string // one line each: the script line and why
}

var (
	// reTestInline is a whole pm.test on one line, the way Postman's
	// snippets and most hand-written tests put a single check: the body is
	// an arrow expression or a braced block.
	reTestInline  = regexp.MustCompile(`^pm\.test\(\s*(?:"[^"]*"|'[^']*'|` + "`[^`]*`" + `)\s*,\s*(?:(?:function\s*\(\s*\)|\(\s*\)\s*=>)\s*\{(.*)\}|\(\s*\)\s*=>\s*(.+?))\s*\)\s*;?$`)
	reTestOpen    = regexp.MustCompile(`^pm\.test\(.*,\s*(?:function\s*\(\s*\)|\(\s*\)\s*=>)\s*\{\s*$`)
	reBlockClose  = regexp.MustCompile(`^\}\s*\)?\s*;?$`)
	reJSONVar     = regexp.MustCompile(`^(?:var|let|const)\s+(\w+)\s*=\s*pm\.response\.json\(\)\s*;?$`)
	reStatus      = regexp.MustCompile(`^pm\.response\.to\.have\.status\((\d+)\)\s*;?$`)
	reStatusText  = regexp.MustCompile(`^pm\.response\.to\.have\.status\(["']([^"']+)["']\)\s*;?$`)
	reCode        = regexp.MustCompile(`^pm\.expect\(pm\.response\.code\)\.to\.(?:eql|equal|be\.equal)\((\d+)\)\s*;?$`)
	reTime        = regexp.MustCompile(`^pm\.expect\(pm\.response\.responseTime\)\.to\.be\.below\((\d+)\)\s*;?$`)
	reHeaderHas   = regexp.MustCompile(`^pm\.response\.to\.have\.header\(["']([^"']+)["']\)\s*;?$`)
	reHeaderIs    = regexp.MustCompile(`^pm\.expect\(pm\.response\.headers\.get\(["']([^"']+)["']\)\)\.to\.(eql|equal|include|contain)\((.+)\)\s*;?$`)
	reTextHas     = regexp.MustCompile(`^pm\.expect\(pm\.response\.text\(\)\)\.to\.(?:include|contain)\((.+)\)\s*;?$`)
	reJSONIs      = regexp.MustCompile(`^pm\.expect\((\w+)((?:\.[\w$]+|\[[^\]]+\])+)\)\.to\.(eql|equal|be\.equal|include|contain|exist|be\.true|be\.false)(?:\((.*)\))?\s*;?$`)
	reJSONInline  = regexp.MustCompile(`^pm\.expect\(pm\.response\.json\(\)((?:\.[\w$]+|\[[^\]]+\])+)\)\.to\.(eql|equal|be\.equal|include|contain|exist|be\.true|be\.false)(?:\((.*)\))?\s*;?$`)
	reSetFromJSON = regexp.MustCompile(`^pm\.(?:environment|collectionVariables|globals|variables)\.set\(["']([^"']+)["'],\s*(\w+)((?:\.[\w$]+|\[[^\]]+\])+)\)\s*;?$`)
	reSetInline   = regexp.MustCompile(`^pm\.(?:environment|collectionVariables|globals|variables)\.set\(["']([^"']+)["'],\s*pm\.response\.json\(\)((?:\.[\w$]+|\[[^\]]+\])+)\)\s*;?$`)
	reSetHeader   = regexp.MustCompile(`^pm\.(?:environment|collectionVariables|globals|variables)\.set\(["']([^"']+)["'],\s*pm\.response\.headers\.get\(["']([^"']+)["']\)\)\s*;?$`)
)

// translateTests reads the lines of a `test` script and turns the forms it
// recognises into assertions and captures. Everything else is reported.
func translateTests(lines []string) translated {
	var out translated
	jsonVars := map[string]bool{}
	for _, raw := range expandInlineTests(lines) {
		line := strings.TrimSpace(raw)
		switch {
		case line == "", strings.HasPrefix(line, "//"), strings.HasPrefix(line, "/*"), strings.HasPrefix(line, "*"):
			continue
		case reTestOpen.MatchString(line), reBlockClose.MatchString(line):
			continue
		}
		if m := reJSONVar.FindStringSubmatch(line); m != nil {
			jsonVars[m[1]] = true
			continue
		}
		if m := reStatus.FindStringSubmatch(line); m != nil {
			out.Asserts = append(out.Asserts, "status == "+m[1])
			continue
		}
		if m := reStatusText.FindStringSubmatch(line); m != nil {
			out.Asserts = append(out.Asserts, "statusText == "+m[1])
			continue
		}
		if m := reCode.FindStringSubmatch(line); m != nil {
			out.Asserts = append(out.Asserts, "status == "+m[1])
			continue
		}
		if m := reTime.FindStringSubmatch(line); m != nil {
			out.Asserts = append(out.Asserts, "duration < "+m[1])
			continue
		}
		if m := reHeaderHas.FindStringSubmatch(line); m != nil {
			out.Asserts = append(out.Asserts, "header."+strings.ToLower(m[1])+" exists")
			continue
		}
		if m := reHeaderIs.FindStringSubmatch(line); m != nil {
			out.Asserts = append(out.Asserts, "header."+strings.ToLower(m[1])+" "+chaiOp(m[2])+" "+literal(m[3]))
			continue
		}
		if m := reTextHas.FindStringSubmatch(line); m != nil {
			out.Asserts = append(out.Asserts, "body contains "+literal(m[1]))
			continue
		}
		if m := reJSONInline.FindStringSubmatch(line); m != nil {
			if a, ok := jsonAssert(m[1], m[2], m[3]); ok {
				out.Asserts = append(out.Asserts, a)
				continue
			}
		}
		if m := reJSONIs.FindStringSubmatch(line); m != nil && jsonVars[m[1]] {
			if a, ok := jsonAssert(m[2], m[3], m[4]); ok {
				out.Asserts = append(out.Asserts, a)
				continue
			}
		}
		if m := reSetInline.FindStringSubmatch(line); m != nil {
			out.Captures = append(out.Captures, m[1]+" = body.$"+m[2])
			continue
		}
		if m := reSetFromJSON.FindStringSubmatch(line); m != nil && jsonVars[m[2]] {
			out.Captures = append(out.Captures, m[1]+" = body.$"+m[3])
			continue
		}
		if m := reSetHeader.FindStringSubmatch(line); m != nil {
			out.Captures = append(out.Captures, m[1]+" = header."+strings.ToLower(m[2]))
			continue
		}
		out.Unsupported = append(out.Unsupported, line)
	}
	return out
}

// expandInlineTests replaces a one-line pm.test with the statements inside
// it, so `pm.test("ok", () => pm.response.to.have.status(200));` reads the
// same as the three-line form. A statement it then cannot translate is
// reported on its own.
func expandInlineTests(lines []string) []string {
	var out []string
	for _, raw := range lines {
		m := reTestInline.FindStringSubmatch(strings.TrimSpace(raw))
		switch {
		case m == nil:
			out = append(out, raw)
		case m[2] != "":
			out = append(out, m[2])
		default:
			for _, stmt := range strings.Split(m[1], ";") {
				if stmt = strings.TrimSpace(stmt); stmt != "" {
					out = append(out, stmt)
				}
			}
		}
	}
	return out
}

// jsonAssert maps a chai expectation on a JSON path onto an assertion.
func jsonAssert(path, op, arg string) (string, bool) {
	sel := "body.$" + path
	switch op {
	case "eql", "equal", "be.equal":
		return sel + " == " + literal(arg), true
	case "include", "contain":
		return sel + " contains " + literal(arg), true
	case "exist":
		return sel + " exists", true
	case "be.true":
		return sel + " == true", true
	case "be.false":
		return sel + " == false", true
	}
	return "", false
}

func chaiOp(op string) string {
	switch op {
	case "include", "contain":
		return "contains"
	}
	return "=="
}

// literal strips the quotes off a JavaScript string literal; numbers,
// booleans and anything else pass through.
func literal(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if q := s[0]; (q == '"' || q == '\'' || q == '`') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}
