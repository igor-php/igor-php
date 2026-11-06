package analyzer

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Narrow exemptions for writes that cannot carry state from one request to the next (#105).

// resetMethodNames are the methods whose empty-value writes are the reset itself. Exact names only:
// prefixes such as resetRequestFormats() can memoize.
var resetMethodNames = map[string]bool{"clear": true, "close": true, "__destruct": true}

// requestScopedWords mark a lazy initializer that could capture the first request's data.
var requestScopedWords = []string{"request", "session", "token"}

// isLazyInit reports `$this->x ??= new Foo(...)` built only from constants and injected services:
// every request would build the same object, so the first one winning is harmless. An argument read
// from a method parameter, a local variable, the request, the session or a token keeps it reported.
func (v *PHPVisitor) isLazyInit(n, left, right *sitter.Node) bool {
	if n.Kind() != "augmented_assignment_expression" || !hasChildKind(n, "??=") {
		return false
	}
	if !v.isThisProperty(left) || right.Kind() != "object_creation_expression" {
		return false
	}

	lower := strings.ToLower(v.getContent(right))
	for _, word := range requestScopedWords {
		if strings.Contains(lower, word) {
			return false
		}
	}

	return !v.hasVariableOtherThanThis(right)
}

// isResetMethodWrite reports an empty value (null, [], 0, false, ”) written to a property in
// clear(), close() or __destruct(): that write is what frees the state. reset() itself is already
// tracked by the reset check.
func (v *PHPVisitor) isResetMethodWrite(n, left, right *sitter.Node) bool {
	if n.Kind() != "assignment_expression" || !resetMethodNames[strings.ToLower(v.curMethod)] {
		return false
	}
	if !v.isThisProperty(left) && !v.isOwnStaticProperty(left) {
		return false
	}
	return v.isEmptyValue(right)
}

// isEnvRead reports a read of $_ENV: it holds the process environment, not request data. Writes
// ($_ENV[...] = ..., unset($_ENV[...])) still poison every later request and stay reported.
func isEnvRead(n *sitter.Node, content func(*sitter.Node) string) bool {
	if content(n) != "$_ENV" {
		return false
	}

	// Climb to the outermost $_ENV[...][...] access
	target := n
	for parent := target.Parent(); parent != nil && parent.Kind() == "subscript_expression" && sameNode(parent.Child(0), target); parent = target.Parent() {
		target = parent
	}

	parent := target.Parent()
	if parent == nil {
		return true
	}
	switch parent.Kind() {
	case "assignment_expression", "augmented_assignment_expression", "reference_assignment_expression":
		return !sameNode(parent.ChildByFieldName("left"), target)
	case "update_expression", "unset_statement":
		return false
	}
	return true
}

func (v *PHPVisitor) isThisProperty(n *sitter.Node) bool {
	if n == nil || n.Kind() != "member_access_expression" {
		return false
	}
	obj := n.ChildByFieldName("object")
	return obj != nil && v.getContent(obj) == "$this"
}

func (v *PHPVisitor) isOwnStaticProperty(n *sitter.Node) bool {
	if n == nil || n.Kind() != "scoped_property_access_expression" {
		return false
	}
	scope := n.ChildByFieldName("scope")
	if scope == nil {
		return false
	}
	switch strings.ToLower(v.getContent(scope)) {
	case "self", "static":
		return true
	}
	return false
}

func (v *PHPVisitor) isEmptyValue(n *sitter.Node) bool {
	switch strings.ToLower(strings.Join(strings.Fields(v.getContent(n)), "")) {
	case "null", "[]", "array()", "0", "false", "''", `""`:
		return true
	}
	return false
}

func (v *PHPVisitor) hasVariableOtherThanThis(n *sitter.Node) bool {
	if n.Kind() == "variable_name" && v.getContent(n) != "$this" {
		return true
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		if v.hasVariableOtherThanThis(n.Child(i)) {
			return true
		}
	}
	return false
}

func hasChildKind(n *sitter.Node, kind string) bool {
	for i := uint(0); i < n.ChildCount(); i++ {
		if n.Child(i).Kind() == kind {
			return true
		}
	}
	return false
}

func sameNode(a, b *sitter.Node) bool {
	return a != nil && b != nil && a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte()
}
