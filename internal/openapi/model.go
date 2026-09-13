package openapi

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// document is a minimal, order-preserving view of an OpenAPI 3 file built
// on yaml.Node. JSON documents parse the same way since JSON is YAML.
type document struct {
	root *yaml.Node
	v31  bool  // OpenAPI 3.1: keys next to $ref override the target; 3.0 ignores them
	err  error // first structural problem met while resolving (cyclic $ref or alias)
}

func parseDocument(data []byte) (*document, error) {
	var n yaml.Node
	if err := yaml.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	root := &n
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("document is not an object")
	}
	d := &document{root: root}
	version := str(d.get(root, "openapi"))
	d.v31 = strings.HasPrefix(version, "3.1")
	if !strings.HasPrefix(version, "3.") {
		if str(d.get(root, "swagger")) != "" {
			return nil, fmt.Errorf("the document is Swagger 2.0, which is not supported; convert it to OpenAPI 3 first")
		}
		return nil, fmt.Errorf("not an OpenAPI 3 document (missing `openapi: 3.x`)")
	}
	return d, nil
}

type kv struct {
	key   string
	value *yaml.Node
}

// entries returns a mapping's key/value pairs in document order, with
// $ref and aliases resolved on the mapping itself.
func (d *document) entries(n *yaml.Node) []kv {
	n = d.resolve(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]kv, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, kv{n.Content[i].Value, n.Content[i+1]})
	}
	return out
}

// get returns the value for key in a mapping, resolved, or nil. Use it for
// OpenAPI objects; free-form data (examples, defaults) goes through getRaw.
func (d *document) get(n *yaml.Node, key string) *yaml.Node {
	for _, e := range d.entries(n) {
		if e.key == key {
			return d.resolve(e.value)
		}
	}
	return nil
}

// getRaw returns the value for key without $ref resolution, so an example
// payload that happens to contain a "$ref" key is kept as data.
func (d *document) getRaw(n *yaml.Node, key string) *yaml.Node {
	for _, e := range d.entries(n) {
		if e.key == key {
			return e.value
		}
	}
	return nil
}

// itemsRaw returns a sequence's elements without $ref resolution.
func (d *document) itemsRaw(n *yaml.Node) []*yaml.Node {
	n = d.resolve(n)
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}

// items returns a sequence's elements, resolved.
func (d *document) items(n *yaml.Node) []*yaml.Node {
	n = d.resolve(n)
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]*yaml.Node, 0, len(n.Content))
	for _, c := range n.Content {
		out = append(out, d.resolve(c))
	}
	return out
}

// resolve follows YAML aliases and local `$ref` pointers such as
// `#/components/schemas/Pet`. A cycle is recorded in d.err and yields nil.
// External references are left unresolved (returned as the mapping holding
// $ref).
func (d *document) resolve(n *yaml.Node) *yaml.Node {
	return d.resolveFrom(n, map[*yaml.Node]bool{})
}

// resolveFrom is resolve with the cycle-detection state threaded through,
// so a 3.1 sibling merge cannot restart the walk on its own target.
func (d *document) resolveFrom(n *yaml.Node, visited map[*yaml.Node]bool) *yaml.Node {
	for n != nil {
		if visited[n] {
			d.fail(fmt.Errorf("cyclic $ref or alias at line %d", n.Line))
			return nil
		}
		visited[n] = true
		if n.Kind == yaml.AliasNode {
			n = n.Alias
			continue
		}
		if n.Kind != yaml.MappingNode {
			return n
		}
		ref := ""
		var siblings []*yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "$ref" {
				ref = n.Content[i+1].Value
				continue
			}
			siblings = append(siblings, n.Content[i], n.Content[i+1])
		}
		if ref == "" || !strings.HasPrefix(ref, "#/") {
			return n
		}
		target := d.pointer(strings.TrimPrefix(ref, "#/"))
		if target == nil {
			d.fail(fmt.Errorf("unresolvable %s at line %d", ref, n.Line))
			return n
		}
		if len(siblings) > 0 && d.v31 {
			// OpenAPI 3.1 allows keys next to $ref; they override the target's.
			// In 3.0 a Reference Object's siblings are ignored.
			target = d.resolveFrom(target, visited)
			if target == nil {
				return nil // cycle already recorded
			}
			if target.Kind == yaml.MappingNode {
				merged := &yaml.Node{Kind: yaml.MappingNode, Tag: target.Tag}
				overridden := map[string]bool{}
				for i := 0; i+1 < len(siblings); i += 2 {
					overridden[siblings[i].Value] = true
				}
				for i := 0; i+1 < len(target.Content); i += 2 {
					if !overridden[target.Content[i].Value] {
						merged.Content = append(merged.Content, target.Content[i], target.Content[i+1])
					}
				}
				merged.Content = append(merged.Content, siblings...)
				return merged
			}
		}
		n = target
	}
	return n
}

func (d *document) fail(err error) {
	if d.err == nil {
		d.err = err
	}
}

// pointer walks a JSON-pointer path (already stripped of `#/`) from the root.
func (d *document) pointer(path string) *yaml.Node {
	cur := d.root
	for _, seg := range strings.Split(path, "/") {
		// Fragments are URI-encoded before JSON Pointer escaping applies.
		if dec, err := url.PathUnescape(seg); err == nil {
			seg = dec
		}
		seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
		if cur.Kind == yaml.AliasNode {
			cur = cur.Alias
		}
		var next *yaml.Node
		switch cur.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(cur.Content); i += 2 {
				if cur.Content[i].Value == seg {
					next = cur.Content[i+1]
					break
				}
			}
		case yaml.SequenceNode:
			idx, err := strconv.Atoi(seg)
			if err == nil && idx >= 0 && idx < len(cur.Content) {
				next = cur.Content[idx]
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

func str(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func boolean(n *yaml.Node) bool {
	return strings.EqualFold(str(n), "true")
}

// decode converts a node into plain Go values, keeping object key order.
// Aliases are followed, but a node already on the current path (a
// recursive anchor) decodes to nil instead of recursing forever.
func decode(n *yaml.Node) any {
	return decodeGuarded(n, map[*yaml.Node]bool{})
}

func decodeGuarded(n *yaml.Node, path map[*yaml.Node]bool) any {
	if n == nil || path[n] {
		return nil
	}
	path[n] = true
	defer delete(path, n)
	switch n.Kind {
	case yaml.AliasNode:
		return decodeGuarded(n.Alias, path)
	case yaml.MappingNode:
		obj := &orderedObject{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			obj.set(n.Content[i].Value, decodeGuarded(n.Content[i+1], path))
		}
		return obj
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, decodeGuarded(c, path))
		}
		return out
	default:
		var v any
		if err := n.Decode(&v); err != nil {
			return n.Value
		}
		return v
	}
}
