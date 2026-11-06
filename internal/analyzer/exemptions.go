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

	// A call (even on an injected service, e.g. $this->identity->getUser()) may return per-request data
	return !v.hasVariableOtherThanThis(right) && !hasCall(right)
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

// envByRefFunctions change the array (or variable) passed to them by reference.
var envByRefFunctions = map[string]bool{
	"array_pop": true, "array_push": true, "array_shift": true, "array_unshift": true, "array_splice": true,
	"array_walk": true, "array_walk_recursive": true, "sort": true, "rsort": true, "usort": true, "uasort": true,
	"uksort": true, "ksort": true, "krsort": true, "asort": true, "arsort": true, "natsort": true,
	"natcasesort": true, "shuffle": true, "array_multisort": true, "settype": true, "parse_str": true,
	"preg_match": true, "preg_match_all": true, "str_replace": true, "extract": true,
}

// isEnvRead reports a read of $_ENV: it holds the process environment, not request data. Writes
// ($_ENV[...] = ..., unset($_ENV[...])), references (=& $_ENV, foreach by reference) and by-reference
// arguments (array_pop($_ENV)) still poison every later request and stay reported.
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
	case "assignment_expression", "augmented_assignment_expression":
		return !sameNode(parent.ChildByFieldName("left"), target)
	case "reference_assignment_expression", "update_expression", "unset_statement", "by_ref":
		// $env = &$_ENV aliases it: writes through $env change the process environment
		return false
	case "foreach_statement":
		return !foreachByRef(parent, content)
	case "argument":
		return !isByRefCallArgument(parent, content)
	}
	return true
}

// foreachByRef reports foreach ($_ENV as &$value) or foreach ($_ENV as $k => &$value).
func foreachByRef(n *sitter.Node, content func(*sitter.Node) string) bool {
	for i := uint(0); i < n.ChildCount(); i++ {
		child := n.Child(i)
		if child.Kind() == "by_ref" || (child.Kind() == "pair" && strings.Contains(content(child), "&")) {
			return true
		}
	}
	return false
}

// isByRefCallArgument reports an argument passed to a function known to change it by reference.
func isByRefCallArgument(argument *sitter.Node, content func(*sitter.Node) string) bool {
	args := argument.Parent()
	if args == nil {
		return false
	}
	call := args.Parent()
	if call == nil || call.Kind() != "function_call_expression" {
		return false
	}
	name := call.ChildByFieldName("function")
	if name == nil {
		return false
	}
	fn := strings.ToLower(content(name))
	return envByRefFunctions[fn[strings.LastIndex(fn, `\`)+1:]]
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
	value := strings.TrimSpace(v.getContent(n))
	// Strings are compared as written: ' ' is not empty
	if value == "''" || value == `""` {
		return true
	}
	if strings.ContainsAny(value, `'"`) {
		return false
	}
	switch strings.ToLower(strings.Join(strings.Fields(value), "")) {
	case "null", "[]", "array()", "0", "false":
		return true
	}
	return false
}

// hasCall reports whether an expression calls a function or a method.
func hasCall(n *sitter.Node) bool {
	switch n.Kind() {
	case "function_call_expression", "member_call_expression", "nullsafe_member_call_expression", "scoped_call_expression":
		return true
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		if hasCall(n.Child(i)) {
			return true
		}
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
