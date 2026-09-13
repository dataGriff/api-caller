package openapi

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// document is a minimal, order-preserving view of an OpenAPI 3 file built
// on yaml.Node. JSON documents parse the same way since JSON is YAML.
type document struct {
	root *yaml.Node
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

// get returns the value for key in a mapping, or nil.
func (d *document) get(n *yaml.Node, key string) *yaml.Node {
	for _, e := range d.entries(n) {
		if e.key == key {
			return d.resolve(e.value)
		}
	}
	return nil
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
// `#/components/schemas/Pet`, up to a small number of hops. External
// references are left unresolved (returned as the mapping holding $ref).
func (d *document) resolve(n *yaml.Node) *yaml.Node {
	for hops := 0; n != nil && hops < 16; hops++ {
		if n.Kind == yaml.AliasNode {
			n = n.Alias
			continue
		}
		if n.Kind != yaml.MappingNode {
			return n
		}
		ref := ""
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "$ref" {
				ref = n.Content[i+1].Value
			}
		}
		if ref == "" || !strings.HasPrefix(ref, "#/") {
			return n
		}
		target := d.pointer(strings.TrimPrefix(ref, "#/"))
		if target == nil {
			return n
		}
		n = target
	}
	return n
}

// pointer walks a JSON-pointer path (already stripped of `#/`) from the root.
func (d *document) pointer(path string) *yaml.Node {
	cur := d.root
	for _, seg := range strings.Split(path, "/") {
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
func decode(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.AliasNode:
		return decode(n.Alias)
	case yaml.MappingNode:
		obj := &orderedObject{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			obj.set(n.Content[i].Value, decode(n.Content[i+1]))
		}
		return obj
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, decode(c))
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
