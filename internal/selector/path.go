package selector

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

// The body path language, the part after `body.$`. It is JSONPath as
// Hurl, Postman and Bruno users write it, evaluated in-tree over gjson
// values so a selected object or array keeps its text exactly as the
// server sent it:
//
//	.name ["name"] ['name']   a key; a bare key runs to the next . or [
//	[2] [-1]                  an index, negative from the end
//	[1:3] [:2] [-2:]          a slice, end exclusive
//	.* [*]                    every element or value
//	..name ..* ..[0]          recursive descent: at any depth
//	[?(@.done == true)]       a filter: == != < <= > >= and =~ /re/i,
//	                          @.x alone for presence, ! for absence,
//	                          && and || between terms
//	.# .length                at the end: how many (an array's elements,
//	                          an object's keys, a string's characters, or
//	                          the matches of a wildcard, slice, filter or
//	                          descent); a number, boolean or null has no
//	                          count; in the middle, # maps over an array
//	                          as gjson did, so items.#.tags.# is each
//	                          item's count
//	.0                        a numeric key on an array is an index, as
//	                          in gjson
//
// A path that goes through a wildcard, slice, filter or descent selects
// every match; the value is then a JSON array of them, and no matches
// means nothing is there.

type segKind int

const (
	segChild   segKind = iota // a key
	segIndex                  // [n]
	segSlice                  // [a:b]
	segWild                   // * — every element or value
	segHash                   // # — count at the end, map in the middle
	segLength                 // .length — a key when the object has one, else count
	segDescend                // .. followed by inner
	segFilter                 // [?(...)]
)

type segment struct {
	kind   segKind
	name   string
	index  int
	lo, hi *int
	inner  *segment // segDescend
	filter orExpr   // segFilter
	text   string   // as written, for errors
}

// Path is a parsed body path.
type Path struct {
	segs []segment
	// relative is a filter's @ path, where .length on an object is only
	// its own key: [?(@.length)] asks whether the key is there.
	relative bool
}

// ParsePath parses the tail of a `body.$` selector: `.items[0].id`,
// `..id`, `["key"]`. An error names the construct it could not read.
func ParsePath(tail string) (*Path, error) {
	p := &Path{}
	i := 0
	for i < len(tail) {
		switch {
		case strings.HasPrefix(tail[i:], ".."):
			i += 2
			inner, n, err := parseStep(tail[i:])
			if err != nil {
				return nil, err
			}
			if inner.kind == segHash || inner.kind == segDescend {
				return nil, fmt.Errorf("unsupported selector part %q: .. must be followed by a key, *, an index or a filter", ".."+tail[i:i+n])
			}
			p.segs = append(p.segs, segment{kind: segDescend, inner: &inner, text: ".." + inner.text})
			i += n
		case tail[i] == '.':
			i++
			seg, n, err := parseStep(tail[i:])
			if err != nil {
				return nil, err
			}
			p.segs = append(p.segs, seg)
			i += n
		case tail[i] == '[':
			seg, n, err := parseBracket(tail[i:])
			if err != nil {
				return nil, err
			}
			p.segs = append(p.segs, seg)
			i += n
		default:
			return nil, fmt.Errorf("unsupported selector part %q: expected . or [ before it", tail[i:])
		}
	}
	return p, nil
}

// parseStep reads what follows a `.` or `..`: a bracket, `*`, `#`, or a
// bare key up to the next `.` or `[`.
func parseStep(s string) (segment, int, error) {
	if s == "" {
		return segment{}, 0, fmt.Errorf("unsupported selector part %q: a path cannot end with a dot", ".")
	}
	if s[0] == '[' {
		return parseBracket(s)
	}
	end := strings.IndexAny(s, ".[")
	if end < 0 {
		end = len(s)
	}
	name := s[:end]
	switch name {
	case "":
		return segment{}, 0, fmt.Errorf("unsupported selector part %q: empty key", "."+s)
	case "*":
		return segment{kind: segWild, text: "*"}, end, nil
	case "#":
		return segment{kind: segHash, text: "#"}, end, nil
	case "length":
		return segment{kind: segLength, name: name, text: name}, end, nil
	}
	if strings.HasPrefix(name, "#(") {
		return segment{}, 0, fmt.Errorf("unsupported selector part %q: gjson queries are not supported; use a filter such as [?(@.id == 2)]", s)
	}
	return segment{kind: segChild, name: name, text: name}, end, nil
}

// parseBracket reads one `[...]`, respecting quotes and regex literals
// inside it, and returns the segment and the bytes consumed.
func parseBracket(s string) (segment, int, error) {
	end := closingBracket(s)
	if end < 0 {
		return segment{}, 0, fmt.Errorf("unsupported selector part %q: no closing ]", s)
	}
	text := s[:end+1]
	inner := strings.TrimSpace(s[1:end])
	switch {
	case inner == "*":
		return segment{kind: segWild, text: text}, end + 1, nil
	case inner == "#":
		return segment{kind: segHash, text: text}, end + 1, nil
	case strings.HasPrefix(inner, "?"):
		expr := strings.TrimSpace(inner[1:])
		if strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
			expr = expr[1 : len(expr)-1]
		}
		f, err := parseFilter(expr)
		if err != nil {
			return segment{}, 0, fmt.Errorf("unsupported filter %q: %w", text, err)
		}
		return segment{kind: segFilter, filter: f, text: text}, end + 1, nil
	case len(inner) >= 2 && (inner[0] == '"' || inner[0] == '\''):
		name, ok := unquote(inner)
		if !ok {
			return segment{}, 0, fmt.Errorf("unsupported selector part %q: a quoted key must be one string", text)
		}
		return segment{kind: segChild, name: name, text: text}, end + 1, nil
	case strings.Contains(inner, ","):
		return segment{}, 0, fmt.Errorf("unsupported selector part %q: unions are not supported; select each value on its own", text)
	case strings.Contains(inner, ":"):
		parts := strings.Split(inner, ":")
		if len(parts) > 2 {
			return segment{}, 0, fmt.Errorf("unsupported selector part %q: slice steps are not supported", text)
		}
		seg := segment{kind: segSlice, text: text}
		for i, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			n, err := strconv.Atoi(part)
			if err != nil {
				return segment{}, 0, fmt.Errorf("unsupported selector part %q: slice bounds must be integers", text)
			}
			if i == 0 {
				seg.lo = &n
			} else {
				seg.hi = &n
			}
		}
		return seg, end + 1, nil
	}
	n, err := strconv.Atoi(inner)
	if err != nil {
		return segment{}, 0, fmt.Errorf("unsupported selector part %q: expected an index, a slice, a quoted key, * or a filter", text)
	}
	return segment{kind: segIndex, index: n, text: text}, end + 1, nil
}

// Leading splits an expression such as `body.$.items[?(@.done == true)].id
// == 2` into the selector at its start and the rest, so spaces inside a
// bracket (a filter, a quoted key) stay part of the selector.
func Leading(s string) (sel, rest string) {
	s = strings.TrimLeft(s, " \t")
	sel = s
	scan(s, false, func(i, depth int) bool {
		if depth == 0 && (s[i] == ' ' || s[i] == '\t') {
			sel, rest = s[:i], strings.TrimLeft(s[i:], " \t")
			return false
		}
		return true
	})
	return sel, rest
}

// closingBracket finds the `]` closing the `[` at s[0], skipping nested
// brackets, quoted strings and the `/regex/` of a filter's =~.
func closingBracket(s string) int {
	end := -1
	scan(s, true, func(i, depth int) bool {
		if s[i] == ']' && depth == 1 {
			end = i
			return false
		}
		return true
	})
	return end
}

// scan calls visit with each byte of s that is not inside a quoted string
// or the `/regex/` of an =~, and the bracket depth before it; visit
// returns false to stop. Quotes and regexes count only inside a bracket
// unless top is set (a filter's text, whose brackets were stripped), so a
// bare key such as it's stays a key.
func scan(s string, top bool, visit func(i, depth int) bool) {
	var quote byte
	inRegex := false
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			continue
		case inRegex:
			switch c {
			case '\\':
				i++
			case '/':
				inRegex = false
			}
			continue
		case (depth > 0 || top) && (c == '"' || c == '\''):
			quote = c
			continue
		case (depth > 0 || top) && c == '/' && strings.HasSuffix(strings.TrimRight(s[:i], " "), "=~"):
			inRegex = true
			continue
		}
		if !visit(i, depth) {
			return
		}
		switch c {
		case '[':
			depth++
		case ']':
			depth--
		}
	}
}

func unquote(s string) (string, bool) {
	if len(s) < 2 || s[len(s)-1] != s[0] {
		return "", false
	}
	if s[0] == '"' {
		v, err := strconv.Unquote(s)
		return v, err == nil
	}
	body := s[1 : len(s)-1]
	if strings.ContainsRune(strings.ReplaceAll(body, `\'`, ""), '\'') {
		return "", false
	}
	return strings.ReplaceAll(strings.ReplaceAll(body, `\'`, "'"), `\\`, `\`), true
}

// Eval evaluates the path on a JSON document. many says the path went
// through a wildcard, slice, filter or descent, so the matches are a set.
func (p *Path) Eval(doc gjson.Result) (nodes []gjson.Result, count *int, many bool) {
	nodes = []gjson.Result{doc}
	mapped := false // through a # in the middle, gjson's items.#.id
	for i, seg := range p.segs {
		last := i == len(p.segs)-1
		switch seg.kind {
		case segHash:
			if last {
				return p.count(nodes, many, mapped)
			}
			nodes, many, mapped = flatMap(nodes, elements), true, true
		case segLength:
			// An object's own length key wins, and in a filter an object's
			// .length is only ever that key.
			key := len(nodes) == 1 && !many && (len(child(nodes[0], "length")) == 1 || p.relative && nodes[0].IsObject())
			if last && !key {
				return p.count(nodes, many, mapped)
			}
			nodes = flatMap(nodes, func(r gjson.Result) []gjson.Result { return child(r, "length") })
		case segChild:
			nodes = flatMap(nodes, func(r gjson.Result) []gjson.Result { return child(r, seg.name) })
		case segIndex:
			nodes = flatMap(nodes, func(r gjson.Result) []gjson.Result { return index(r, seg.index) })
		case segSlice:
			nodes, many = flatMap(nodes, func(r gjson.Result) []gjson.Result { return slice(r, seg.lo, seg.hi) }), true
		case segWild:
			nodes, many = flatMap(nodes, elements), true
		case segFilter:
			nodes, many = flatMap(nodes, func(r gjson.Result) []gjson.Result { return filter(r, seg.filter) }), true
		case segDescend:
			nodes, many = flatMap(nodes, func(r gjson.Result) []gjson.Result {
				var out []gjson.Result
				descend(r, func(d gjson.Result) { out = append(out, step(d, *seg.inner)...) })
				return out
			}), true
		}
		if len(nodes) == 0 {
			if i+1 < len(p.segs) && isCount(p.segs[len(p.segs)-1]) && many && !mapped {
				// Counting matches that turned out empty is 0, not absent.
				zero := 0
				return nil, &zero, many
			}
			return nil, nil, many
		}
	}
	return nodes, nil, many
}

// count ends a path with # or .length: the number of matches of a set,
// each node's own count after a mapping #, or the one node's count.
func (p *Path) count(nodes []gjson.Result, many, mapped bool) ([]gjson.Result, *int, bool) {
	switch {
	case mapped:
		var out []gjson.Result
		for _, n := range nodes {
			if c, ok := p.countOne(n); ok {
				out = append(out, gjson.Result{Type: gjson.Number, Raw: strconv.Itoa(c), Num: float64(c)})
			}
		}
		if len(out) == 0 {
			return nil, nil, true
		}
		return out, nil, true
	case many:
		n := len(nodes)
		return nil, &n, true
	}
	c, ok := p.countOne(nodes[0])
	if !ok {
		return nil, nil, false
	}
	return nil, &c, false
}

// countOne is an array's elements, an object's keys or a string's
// characters; a number, boolean or null has no count. In a filter an
// object's keys are not counted, so @.length is the key alone.
func (p *Path) countOne(r gjson.Result) (int, bool) {
	n := 0
	switch {
	case r.IsArray() || r.IsObject() && !p.relative:
		r.ForEach(func(_, _ gjson.Result) bool { n++; return true })
	case r.Type == gjson.String:
		n = utf8.RuneCountInString(r.Str)
	default:
		return 0, false
	}
	return n, true
}

func isCount(s segment) bool { return s.kind == segHash || s.kind == segLength }

// step applies one non-descent segment to one node, for `..`.
func step(r gjson.Result, seg segment) []gjson.Result {
	switch seg.kind {
	case segChild, segLength:
		return child(r, seg.name)
	case segIndex:
		return index(r, seg.index)
	case segSlice:
		return slice(r, seg.lo, seg.hi)
	case segWild:
		return elements(r)
	case segFilter:
		return filter(r, seg.filter)
	}
	return nil
}

func flatMap(nodes []gjson.Result, f func(gjson.Result) []gjson.Result) []gjson.Result {
	var out []gjson.Result
	for _, n := range nodes {
		out = append(out, f(n)...)
	}
	return out
}

// child is the value under key in an object, compared exactly (no gjson
// path syntax, so dots and stars in keys are literal). On an array a key
// of digits is an index, as gjson's items.0 was.
func child(r gjson.Result, key string) []gjson.Result {
	if r.IsArray() {
		if i, err := strconv.Atoi(key); err == nil && i >= 0 && key[0] != '+' {
			return index(r, i)
		}
		return nil
	}
	if !r.IsObject() {
		return nil
	}
	var out []gjson.Result
	r.ForEach(func(k, v gjson.Result) bool {
		if k.Str == key {
			out = []gjson.Result{v}
			return false
		}
		return true
	})
	return out
}

// size is how many elements an array has, without building them.
func size(r gjson.Result) int {
	n := 0
	r.ForEach(func(_, _ gjson.Result) bool { n++; return true })
	return n
}

// index walks to element i and stops there; a negative i counts first.
func index(r gjson.Result, i int) []gjson.Result {
	if !r.IsArray() {
		return nil
	}
	if i < 0 {
		i += size(r)
		if i < 0 {
			return nil
		}
	}
	var out []gjson.Result
	n := 0
	r.ForEach(func(_, v gjson.Result) bool {
		if n == i {
			out = []gjson.Result{v}
			return false
		}
		n++
		return true
	})
	return out
}

// slice walks elements a to b and stops at b; only negative or omitted
// bounds that need the length count first.
func slice(r gjson.Result, lo, hi *int) []gjson.Result {
	if !r.IsArray() {
		return nil
	}
	n := -1
	length := func() int {
		if n < 0 {
			n = size(r)
		}
		return n
	}
	bound := func(p *int, def func() int) int {
		if p == nil {
			return def()
		}
		v := *p
		if v < 0 {
			v = max(0, v+length())
		}
		return v
	}
	a := bound(lo, func() int { return 0 })
	b := bound(hi, length)
	if a >= b {
		return nil
	}
	var out []gjson.Result
	i := 0
	r.ForEach(func(_, v gjson.Result) bool {
		if i >= a {
			out = append(out, v)
		}
		i++
		return i < b
	})
	return out
}

func elements(r gjson.Result) []gjson.Result {
	if !r.IsArray() && !r.IsObject() {
		return nil
	}
	var out []gjson.Result
	r.ForEach(func(_, v gjson.Result) bool { out = append(out, v); return true })
	return out
}

// descend visits r and everything under it, in document order, without
// collecting the whole tree first.
func descend(r gjson.Result, visit func(gjson.Result)) {
	visit(r)
	if r.IsArray() || r.IsObject() {
		r.ForEach(func(_, v gjson.Result) bool { descend(v, visit); return true })
	}
}

func filter(r gjson.Result, f orExpr) []gjson.Result {
	if !r.IsArray() && !r.IsObject() {
		return nil
	}
	var out []gjson.Result
	r.ForEach(func(_, e gjson.Result) bool {
		if f.match(e) {
			out = append(out, e)
		}
		return true
	})
	return out
}

// Filters: terms joined by && (binding tighter) and ||.

type orExpr []andExpr
type andExpr []term

type term struct {
	not   bool
	path  *Path // relative to @
	op    string
	value literal
}

type literal struct {
	kind  gjson.Type // String, Number, True, False, Null
	str   string
	num   *big.Rat
	regex *regexp.Regexp
}

func (o orExpr) match(r gjson.Result) bool {
	for _, a := range o {
		ok := true
		for _, t := range a {
			if !t.match(r) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (t term) match(r gjson.Result) bool {
	nodes, count, _ := t.path.Eval(r)
	if count != nil {
		nodes = []gjson.Result{{Type: gjson.Number, Raw: strconv.Itoa(*count), Num: float64(*count)}}
	}
	var ok bool
	switch {
	case t.op == "":
		ok = len(nodes) > 0
	case len(nodes) == 0:
		// Nothing there is not equal to anything (RFC 9535), and no other
		// comparison holds for it.
		ok = t.op == "!="
	default:
		for _, n := range nodes {
			if compareLiteral(n, t.op, t.value) {
				ok = true
				break
			}
		}
	}
	return ok != t.not
}

func compareLiteral(n gjson.Result, op string, lit literal) bool {
	if op == "=~" {
		return n.Type == gjson.String && lit.regex.MatchString(n.Str)
	}
	cmp, comparable := 0, false
	switch {
	case lit.kind == gjson.Number && n.Type == gjson.Number:
		if v, ok := new(big.Rat).SetString(n.Raw); ok {
			cmp, comparable = v.Cmp(lit.num), true
		}
	case lit.kind == gjson.String && n.Type == gjson.String:
		cmp, comparable = strings.Compare(n.Str, lit.str), true
	case lit.kind == n.Type: // true, false, null
		cmp, comparable = 0, true
	}
	// Only numbers and strings have an order.
	ordered := comparable && (lit.kind == gjson.Number || lit.kind == gjson.String)
	switch op {
	case "==":
		return comparable && cmp == 0
	case "!=":
		return !comparable || cmp != 0
	case "<":
		return ordered && cmp < 0
	case "<=":
		return ordered && cmp <= 0
	case ">":
		return ordered && cmp > 0
	case ">=":
		return ordered && cmp >= 0
	}
	return false
}

var filterOps = []string{"==", "!=", "<=", ">=", "=~", "<", ">"}

func parseFilter(s string) (orExpr, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("the filter is empty")
	}
	var or orExpr
	for _, alt := range splitOutside(s, "||") {
		var and andExpr
		for _, part := range splitOutside(alt, "&&") {
			t, err := parseTerm(strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}
			and = append(and, t)
		}
		or = append(or, and)
	}
	return or, nil
}

// splitOutside splits s on sep where sep is not inside a bracket, quotes
// or a regex.
func splitOutside(s, sep string) []string {
	var parts []string
	start := 0
	scan(s, true, func(i, depth int) bool {
		if depth == 0 && i >= start && strings.HasPrefix(s[i:], sep) {
			parts = append(parts, s[start:i])
			start = i + len(sep)
		}
		return true
	})
	return append(parts, s[start:])
}

func parseTerm(s string) (term, error) {
	var t term
	if strings.HasPrefix(s, "!") {
		t.not = true
		s = strings.TrimSpace(s[1:])
	}
	if strings.HasPrefix(s, "(") {
		return t, fmt.Errorf("parentheses inside a filter are not supported")
	}
	if !strings.HasPrefix(s, "@") {
		return t, fmt.Errorf("a term must start with @, got %q", s)
	}
	// The path runs to the first operator outside a bracket.
	pathEnd, op, bad := len(s), "", false
	scan(s, true, func(i, depth int) bool {
		if i == 0 || depth > 0 || !strings.ContainsRune(" =!<>", rune(s[i])) {
			return true
		}
		rest := strings.TrimLeft(s[i:], " ")
		for _, o := range filterOps {
			if strings.HasPrefix(rest, o) {
				pathEnd, op = i, o
				return false
			}
		}
		bad = s[i] != ' '
		return !bad
	})
	if bad {
		return t, fmt.Errorf("unknown operator in %q", s)
	}
	path, err := ParsePath(strings.TrimSpace(s[1:pathEnd]))
	if err != nil {
		return t, err
	}
	path.relative = true
	t.path = path
	if op == "" {
		if strings.TrimSpace(s[1:]) != strings.TrimSpace(s[1:pathEnd]) {
			return t, fmt.Errorf("expected an operator in %q", s)
		}
		return t, nil
	}
	if t.not {
		return t, fmt.Errorf("! only negates a presence test such as !@.done")
	}
	t.op = op
	raw := strings.TrimSpace(strings.TrimLeft(s[pathEnd:], " ")[len(op):])
	lit, err := parseLiteral(raw, op == "=~")
	if err != nil {
		return t, err
	}
	t.value = lit
	return t, nil
}

func parseLiteral(s string, regex bool) (literal, error) {
	if regex {
		if len(s) < 2 || s[0] != '/' {
			return literal{}, fmt.Errorf("=~ needs a /regex/, got %q", s)
		}
		end := strings.LastIndexByte(s, '/')
		if end == 0 {
			return literal{}, fmt.Errorf("=~ needs a closing / in %q", s)
		}
		pattern, flags := s[1:end], s[end+1:]
		if flags != "" && flags != "i" {
			return literal{}, fmt.Errorf("the only regex flag is i, got %q", flags)
		}
		if flags == "i" {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return literal{}, fmt.Errorf("bad regex %q: %w", s, err)
		}
		return literal{regex: re}, nil
	}
	switch s {
	case "true":
		return literal{kind: gjson.True}, nil
	case "false":
		return literal{kind: gjson.False}, nil
	case "null":
		return literal{kind: gjson.Null}, nil
	}
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') {
		v, ok := unquote(s)
		if !ok {
			return literal{}, fmt.Errorf("bad string %s", s)
		}
		return literal{kind: gjson.String, str: v}, nil
	}
	if n, ok := new(big.Rat).SetString(s); ok && !strings.ContainsAny(s, "/") {
		return literal{kind: gjson.Number, num: n}, nil
	}
	return literal{}, fmt.Errorf("expected a number, a quoted string, true, false or null, got %q", s)
}
