package analyzer

import (
	"strings"

	"github.com/igor-php/igor-php/pkg/symbol"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// tagKeyedCacheWrite marks the findings an assignment produced (from index before) as a keyed cache
// write when it stores into $this->prop[$key] or self::$prop[$key] and looks like memoization: the
// property is named after a cache, or the value is computed once with ??=. The finding stays as it
// is (same message, same severity): the category and the key only change how it is presented, so
// the reviewer can check that no key derives from the request.
func (v *PHPVisitor) tagKeyedCacheWrite(assignment, left *sitter.Node, before int) {
	if len(v.findings) == before {
		return
	}
	prop, key, ok := v.keyedCacheTarget(left)
	if !ok {
		return
	}
	lowerProp := strings.ToLower(prop)
	// ??= computes a value once per key; initializing a counter or a list (??= 0, ??= []) is no memoization
	memoized := assignment.Kind() == "augmented_assignment_expression" && hasChildOfKind(assignment, "??=") && !isLiteral(assignment.ChildByFieldName("right"))
	if !memoized && !strings.Contains(lowerProp, "cache") && !strings.Contains(lowerProp, "memo") {
		return
	}
	for i := before; i < len(v.findings); i++ {
		v.findings[i].Category = symbol.CategoryCache
		v.findings[i].Property = prop
		v.findings[i].CacheKey = key
	}
}

// keyedCacheTarget returns the property and the key expression of a write to $this->prop[$k]...
// or self::$prop[$k]...; every dimension must have a key ($this->prop[] = ... appends).
func (v *PHPVisitor) keyedCacheTarget(left *sitter.Node) (string, string, bool) {
	if left == nil || left.Kind() != "subscript_expression" {
		return "", "", false
	}
	root := left
	for root.Kind() == "subscript_expression" {
		if root.NamedChildCount() < 2 {
			return "", "", false // $x[] has no key
		}
		root = root.NamedChild(0)
	}

	prop := ""
	switch root.Kind() {
	case "member_access_expression":
		if obj := root.ChildByFieldName("object"); obj == nil || v.getContent(obj) != "$this" {
			return "", "", false
		}
		if name := root.ChildByFieldName("name"); name != nil {
			prop = v.getContent(name)
		}
	case "scoped_property_access_expression":
		scope := root.ChildByFieldName("scope")
		name := root.ChildByFieldName("name")
		if scope == nil || name == nil {
			return "", "", false
		}
		if s := strings.ToLower(v.getContent(scope)); s != "self" && s != "static" {
			return "", "", false
		}
		prop = strings.TrimPrefix(v.getContent(name), "$")
	}
	if prop == "" {
		return "", "", false
	}
	key := strings.TrimSpace(strings.TrimPrefix(v.getContent(left), v.getContent(root)))
	return prop, key, true
}

// hasChildOfKind reports whether one of the node's direct children has the given kind.
func hasChildOfKind(n *sitter.Node, kind string) bool {
	for i := uint(0); i < n.ChildCount(); i++ {
		if n.Child(i).Kind() == kind {
			return true
		}
	}
	return false
}

// isLiteral reports whether a value is a constant literal: a number, a string without
// interpolation, a boolean, null, or an array of such literals.
func isLiteral(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind() {
	case "integer", "float", "string", "boolean", "null":
		return true
	case "encapsed_string":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			switch n.NamedChild(i).Kind() {
			case "string_content", "string_value", "escape_sequence":
			default:
				return false
			}
		}
		return true
	case "array_creation_expression":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			element := n.NamedChild(i)
			for j := uint(0); j < element.NamedChildCount(); j++ {
				if !isLiteral(element.NamedChild(j)) {
					return false
				}
			}
		}
		return true
	}
	return false
}
