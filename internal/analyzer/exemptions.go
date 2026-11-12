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

// isExemptWrite reports an assignment that cannot carry state from one request to the next (#105).
func (v *PHPVisitor) isExemptWrite(n, left, right *sitter.Node) bool {
	if left == nil || right == nil {
		return false
	}
	return v.isLazyInit(n, left, right) || v.isResetMethodWrite(n, left, right) || v.isResetMethodDestructuring(n, left, right)
}

// isResetMethodDestructuring is isResetMethodWrite for a destructuring: [$this->a, [$this->b]] =
// [null, [[]]] in clear(), close() or __destruct() empties each property it writes. Every target
// must be a property (or a local) given an empty value: [$this->items[]] = [null] appends.
func (v *PHPVisitor) isResetMethodDestructuring(n, left, right *sitter.Node) bool {
	if n.Kind() != "assignment_expression" || left.Kind() != "list_literal" {
		return false
	}
	return resetMethodNames[strings.ToLower(v.curMethod)] && v.emptiesTargets(left, right)
}

// emptiesTargets reports a destructuring target given an empty value: a property of $this or a
// static of the class given an empty value (or nothing), a local given anything, or a nested
// destructuring whose own targets all get one. Keyed forms are not matched.
func (v *PHPVisitor) emptiesTargets(target, value *sitter.Node) bool {
	switch {
	case target == nil:
		// A skipped element: [, $this->a]
		return true
	case target.Kind() == "variable_name":
		return true
	case target.Kind() == "list_literal":
		return v.emptiesList(target, value)
	case v.isThisProperty(target) || v.isOwnStaticProperty(target):
		return value == nil || v.isEmptyValue(value)
	}
	// $this->items[] or $this->map['key'] adds an entry, by_ref keeps a reference
	return false
}

// emptiesList matches the elements of a destructuring with those of the array literal it is given,
// by position.
func (v *PHPVisitor) emptiesList(list, value *sitter.Node) bool {
	targets, keyed := listSlots(list)
	if keyed {
		return false
	}
	var values []*sitter.Node
	if value != nil {
		if value.Kind() != "array_creation_expression" {
			return false
		}
		for i := uint(0); i < value.NamedChildCount(); i++ {
			element := value.NamedChild(i)
			if element.NamedChildCount() != 1 {
				// 'key' => value, ...$spread
				return false
			}
			values = append(values, element.NamedChild(0))
		}
	}
	for i, target := range targets {
		var assigned *sitter.Node
		if i < len(values) {
			assigned = values[i]
		}
		if !v.emptiesTargets(target, assigned) {
			return false
		}
	}
	return true
}

// listSlots lists the elements of a destructuring by position, nil for a skipped one, and reports
// a keyed form.
func listSlots(list *sitter.Node) (slots []*sitter.Node, keyed bool) {
	var current *sitter.Node
	open := false
	for i := uint(0); i < list.ChildCount(); i++ {
		c := list.Child(i)
		switch {
		case c.Kind() == "=>":
			keyed = true
		case c.Kind() == "[" || c.Kind() == "(":
			open = true
		case c.Kind() == "," || c.Kind() == "]" || c.Kind() == ")":
			if open && (c.Kind() == "," || current != nil) {
				slots = append(slots, current)
			}
			current = nil
		case c.IsNamed():
			current = c
		}
	}
	return slots, keyed
}

// destructuringTargets lists the variables a destructuring writes, in order. A keyed form
// (['id' => $this->id]) puts each key before its target: the keys are read, not written.
func destructuringTargets(list *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	for i := uint(0); i < list.ChildCount(); i++ {
		c := list.Child(i)
		if !c.IsNamed() {
			continue
		}
		if next := list.Child(i + 1); next != nil && next.Kind() == "=>" {
			continue
		}
		out = append(out, c)
	}
	return out
}

// foreachTargets lists what a foreach writes on each iteration: its value, and its key when it
// has one. They may be a property ($this->current), a destructuring or a reference.
func foreachTargets(foreach *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	for i := uint(1); i < foreach.NamedChildCount(); i++ {
		c := foreach.NamedChild(i)
		if c.Kind() == "compound_statement" || c.Kind() == "colon_block" || strings.HasSuffix(c.Kind(), "_statement") {
			break
		}
		if c.Kind() == "pair" {
			for j := uint(0); j < c.NamedChildCount(); j++ {
				out = append(out, c.NamedChild(j))
			}
			continue
		}
		out = append(out, c)
	}
	return out
}

// byRefParam names the parameter a function changes by reference: its position, or -1 when every
// argument is passed by reference, and its name for named arguments.
type byRefParam struct {
	index int
	name  string
}

// envByRefFunctions change the array (or variable) passed to them by reference. Only the argument
// in that position is a write: preg_match('/x/', $_ENV['APP_ENV']) only reads the environment.
var envByRefFunctions = map[string]byRefParam{
	"array_pop": {0, "array"}, "array_push": {0, "array"}, "array_shift": {0, "array"}, "array_unshift": {0, "array"},
	"array_splice": {0, "array"}, "array_walk": {0, "array"}, "array_walk_recursive": {0, "array"},
	"sort": {0, "array"}, "rsort": {0, "array"}, "usort": {0, "array"}, "uasort": {0, "array"}, "uksort": {0, "array"},
	"ksort": {0, "array"}, "krsort": {0, "array"}, "asort": {0, "array"}, "arsort": {0, "array"},
	"natsort": {0, "array"}, "natcasesort": {0, "array"}, "shuffle": {0, "array"}, "extract": {0, "array"},
	"array_multisort": {-1, ""}, "settype": {0, "var"}, "parse_str": {1, "result"},
	"preg_match": {2, "matches"}, "preg_match_all": {2, "matches"}, "str_replace": {3, "count"},
	"str_ireplace": {3, "count"},
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

// isByRefCallArgument reports an argument passed in the by-reference position of a function known
// to change it.
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
	param, found := envByRefFunctions[fn[strings.LastIndex(fn, `\`)+1:]]
	if !found {
		return false
	}
	if param.index < 0 {
		return true
	}
	if argName := argument.ChildByFieldName("name"); argName != nil {
		return strings.EqualFold(content(argName), param.name)
	}
	position := 0
	for i := uint(0); i < args.NamedChildCount(); i++ {
		arg := args.NamedChild(i)
		if sameNode(arg, argument) {
			return position == param.index
		}
		if arg.Kind() == "argument" {
			position++
		}
	}
	return false
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
