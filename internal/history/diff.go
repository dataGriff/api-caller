package history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// Change is one difference between two recorded responses.
type Change struct {
	// Path is "status"; a JSONPath into a JSON body, such as
	// $.user.roles[2]; "line N" for a text body; or "body" when the bodies
	// cannot be compared piece by piece (binary, or too long to diff).
	Path string `json:"path"`
	Op   string `json:"op"` // "added", "removed" or "changed"
	// From and To are the values as JSON: a JSON body's values as they
	// are, a text line as a string. One of them is absent for an addition
	// or a removal, and both for a "body" change.
	From json.RawMessage `json:"from,omitempty"`
	To   json.RawMessage `json:"to,omitempty"`
}

// Change operations.
const (
	Added   = "added"
	Removed = "removed"
	Changed = "changed"
)

// maxLineCells bounds the line diff of text bodies: past it, two bodies
// that differ are reported as one "body" change.
const maxLineCells = 4_000_000

type stored struct {
	Response *struct {
		Status       int             `json:"status"`
		Body         json.RawMessage `json:"body"`
		BodyEncoding string          `json:"body_encoding"`
	} `json:"response"`
}

// Compare lists what changed from one recorded result to another: the
// status, then the body. JSON bodies are compared by structure, object
// keys in sorted order and arrays index by index; text bodies line by
// line. Headers are left out, since a Date or a request id differs every
// time.
func Compare(from, to json.RawMessage) ([]Change, error) {
	var a, b stored
	if err := json.Unmarshal(from, &a); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(to, &b); err != nil {
		return nil, err
	}
	if a.Response == nil || b.Response == nil {
		return nil, fmt.Errorf("an entry has no response")
	}
	var out []Change
	if a.Response.Status != b.Response.Status {
		out = append(out, Change{Path: "status", Op: Changed, From: raw(a.Response.Status), To: raw(b.Response.Status)})
	}
	ab, bb := a.Response.Body, b.Response.Body
	switch {
	case a.Response.BodyEncoding != "" || b.Response.BodyEncoding != "":
		if a.Response.BodyEncoding != b.Response.BodyEncoding || !bytes.Equal(ab, bb) {
			out = append(out, Change{Path: "body", Op: Changed})
		}
	case isString(ab) && isString(bb):
		var as, bs string
		_ = json.Unmarshal(ab, &as)
		_ = json.Unmarshal(bb, &bs)
		out = append(out, diffLines(as, bs)...)
	default:
		av, err := decode(ab)
		if err != nil {
			return nil, err
		}
		bv, err := decode(bb)
		if err != nil {
			return nil, err
		}
		diffValue("$", av, bv, &out)
	}
	return out, nil
}

func isString(r json.RawMessage) bool {
	r = bytes.TrimSpace(r)
	return len(r) > 0 && r[0] == '"'
}

// decode reads a JSON value keeping numbers as they were written; an
// absent body is null.
func decode(r json.RawMessage) (any, error) {
	if len(bytes.TrimSpace(r)) == 0 {
		return nil, nil
	}
	d := json.NewDecoder(bytes.NewReader(r))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

func diffValue(path string, a, b any, out *[]Change) {
	switch av := a.(type) {
	case map[string]any:
		if bv, ok := b.(map[string]any); ok {
			keys := make([]string, 0, len(av)+len(bv))
			for k := range av {
				keys = append(keys, k)
			}
			for k := range bv {
				if _, dup := av[k]; !dup {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				x, inA := av[k]
				y, inB := bv[k]
				p := path + key(k)
				switch {
				case !inA:
					*out = append(*out, Change{Path: p, Op: Added, To: raw(y)})
				case !inB:
					*out = append(*out, Change{Path: p, Op: Removed, From: raw(x)})
				default:
					diffValue(p, x, y, out)
				}
			}
			return
		}
	case []any:
		if bv, ok := b.([]any); ok {
			for i := 0; i < len(av) || i < len(bv); i++ {
				p := fmt.Sprintf("%s[%d]", path, i)
				switch {
				case i >= len(av):
					*out = append(*out, Change{Path: p, Op: Added, To: raw(bv[i])})
				case i >= len(bv):
					*out = append(*out, Change{Path: p, Op: Removed, From: raw(av[i])})
				default:
					diffValue(p, av[i], bv[i], out)
				}
			}
			return
		}
	}
	if !reflect.DeepEqual(a, b) {
		*out = append(*out, Change{Path: path, Op: Changed, From: raw(a), To: raw(b)})
	}
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// key is the JSONPath step to an object member, in the form apic's
// selectors read: .name, or ['odd key'] for anything else.
func key(k string) string {
	if identifier.MatchString(k) {
		return "." + k
	}
	return "['" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(k) + "']"
}

// diffLines is a line diff of two texts by longest common subsequence.
// An added line is numbered as in the newer text, a removed one as in
// the older.
func diffLines(a, b string) []Change {
	if a == b {
		return nil
	}
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(al)*len(bl) > maxLineCells {
		return []Change{{Path: "body", Op: Changed}}
	}
	// lcs[i][j] is the common length of al[i:] and bl[j:].
	lcs := make([][]int, len(al)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(bl)+1)
	}
	for i := len(al) - 1; i >= 0; i-- {
		for j := len(bl) - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []Change
	i, j := 0, 0
	for i < len(al) || j < len(bl) {
		switch {
		case i < len(al) && j < len(bl) && al[i] == bl[j]:
			i++
			j++
		case i < len(al) && (j == len(bl) || lcs[i+1][j] >= lcs[i][j+1]):
			out = append(out, Change{Path: fmt.Sprintf("line %d", i+1), Op: Removed, From: raw(al[i])})
			i++
		default:
			out = append(out, Change{Path: fmt.Sprintf("line %d", j+1), Op: Added, To: raw(bl[j])})
			j++
		}
	}
	return out
}
