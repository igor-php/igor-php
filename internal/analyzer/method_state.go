package analyzer

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// MethodCallRef names a method called on another object, by the class its
// receiver is typed with. Class is empty when that type is unknown.
type MethodCallRef struct {
	Class  string
	Method string
	// Mutator is true when the method name suggests a change: it decides for a class that was not audited.
	Mutator bool
}

// MethodState summarizes whether a method body can change the state of its own
// object or global state. It lets the auditor tell a setter of a stateless service
// (one that only writes to the database) from a real runtime mutation.
type MethodState struct {
	// Mutates is true when the body writes a property of $this, a static property,
	// a global or a static variable, or does something the summary cannot follow.
	Mutates bool
	// MutatesArgs is true when the body changes an object passed as an argument (a mutating call
	// or a property write on a parameter, or a parameter handed to a mutating call). It matters
	// when the caller passes its own shared state.
	MutatesArgs bool
	// SelfCalls are the methods called on $this, self:: or static::.
	SelfCalls []string
	// ParentCalls are the methods called through parent::.
	ParentCalls []string
	// DepCalls are the methods called on a property of $this.
	DepCalls []MethodCallRef
}

// ClassShape is what the auditor needs to know about a class to resolve a call on it.
type ClassShape struct {
	Abstract   bool
	UsesTraits bool
}

// stateRecorder is implemented by engines that collect method state summaries.
type stateRecorder interface {
	RecordClassShape(className string, shape ClassShape)
	RecordMethodState(className, methodName string, state MethodState)
}

// recordClassShape reports the shape of a class_declaration to the engine.
func (v *PHPVisitor) recordClassShape(n *sitter.Node, fullName string) {
	rec, ok := v.engine.(stateRecorder)
	if !ok || n.Kind() != "class_declaration" {
		return
	}
	shape := ClassShape{}
	for i := uint(0); i < n.ChildCount(); i++ {
		if n.Child(i).Kind() == "abstract_modifier" {
			shape.Abstract = true
		}
	}
	if body := v.getClassBody(n); body != nil {
		for i := uint(0); i < body.ChildCount(); i++ {
			if body.Child(i).Kind() == "use_declaration" {
				shape.UsesTraits = true
			}
		}
	}
	rec.RecordClassShape(fullName, shape)
}

// recordMethodState summarizes a method of a class_declaration and reports it to the engine.
// Methods without a body (abstract) and methods of traits, interfaces and enums are not recorded,
// so a call that resolves to them stays unresolved.
func (v *PHPVisitor) recordMethodState(n *sitter.Node) {
	rec, ok := v.engine.(stateRecorder)
	if !ok || v.curClass == "" || v.curMethod == "" {
		return
	}
	list := n.Parent()
	if list == nil || list.Parent() == nil || list.Parent().Kind() != "class_declaration" {
		return
	}
	body := n.ChildByFieldName("body")
	if body == nil {
		return
	}
	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}
	rec.RecordMethodState(fullName, v.curMethod, v.summarizeMethodState(n))
}

// stateSummarizer walks one method body to build its MethodState.
type stateSummarizer struct {
	v  *PHPVisitor
	st MethodState
	// tainted are local variables holding an object reached from $this (or a static property):
	// calling a mutating method on them mutates the service's state.
	tainted map[string]bool
	// params are the method's parameter names.
	params map[string]bool
}

// summarizeMethodState walks a method declaration. It errs on the side of Mutates: anything
// it cannot follow (dynamic calls, references, objects reached from $this through a local
// variable) counts as a mutation.
func (v *PHPVisitor) summarizeMethodState(method *sitter.Node) MethodState {
	s := &stateSummarizer{v: v, tainted: map[string]bool{}, params: map[string]bool{}}
	if list := method.ChildByFieldName("parameters"); list != nil {
		for i := uint(0); i < list.NamedChildCount(); i++ {
			if name := list.NamedChild(i).ChildByFieldName("name"); name != nil {
				s.params[v.getContent(name)] = true
			}
		}
	}
	s.walk(method.ChildByFieldName("body"))
	return s.st
}

func (s *stateSummarizer) walk(n *sitter.Node) {
	if n == nil || s.st.Mutates {
		return
	}
	switch n.Kind() {
	case "class_declaration", "anonymous_class":
		// A nested class has its own $this
		return
	case "member_call_expression", "nullsafe_member_call_expression":
		s.memberCall(n)
	case "scoped_call_expression":
		s.v.summarizeScopedCall(n, &s.st)
	default:
		if s.writes(n) {
			s.st.Mutates = true
			return
		}
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		s.walk(n.Child(i))
	}
}

// writes reports a node that changes the object's own state or global state.
func (s *stateSummarizer) writes(n *sitter.Node) bool {
	v := s.v
	switch n.Kind() {
	case "static_variable_declaration", "global_declaration":
		return true
	case "assignment_expression", "augmented_assignment_expression":
		return s.assignment(n)
	case "reference_assignment_expression":
		// $x = &$this->prop, or $this->prop = &$x
		return v.derivesFromOwnState(n.ChildByFieldName("left")) || v.derivesFromOwnState(n.ChildByFieldName("right"))
	case "update_expression":
		return v.writesOwnState(n.ChildByFieldName("argument"))
	case "unset_statement":
		for i := uint(0); i < n.ChildCount(); i++ {
			if v.writesOwnState(n.Child(i)) {
				return true
			}
		}
	case "foreach_statement":
		// foreach ($this->items as $item) $item->setX() mutates objects the service holds,
		// and foreach ($this->items as &$item) writes the property itself
		return n.ChildCount() > 2 && v.derivesFromOwnState(n.Child(2)) && v.collectVariables(n, s.tainted)
	case "by_ref":
		// [&$this->prop], fn(&$x) bound to a property
		return n.ChildCount() > 0 && v.derivesFromOwnState(n.Child(n.ChildCount()-1))
	case "function_call_expression":
		return v.callWritesOwnState(n)
	}
	return false
}

func (s *stateSummarizer) assignment(n *sitter.Node) bool {
	v := s.v
	left := n.ChildByFieldName("left")
	if left == nil {
		return false
	}
	if v.writesOwnState(left) {
		return true
	}
	if left.Kind() == "variable_name" {
		if v.derivesFromOwnState(n.ChildByFieldName("right")) {
			s.tainted[v.getContent(left)] = true
		}
	} else if s.params[rootVariable(v, left)] {
		s.st.MutatesArgs = true
	}
	return false
}

func (s *stateSummarizer) memberCall(n *sitter.Node) {
	v, st := s.v, &s.st
	obj := n.ChildByFieldName("object")
	nameNode := n.ChildByFieldName("name")
	if obj == nil || nameNode == nil {
		return
	}
	if nameNode.Kind() != "name" {
		// $this->{$method}(), $this->$method()
		if isThisVariable(v, obj) {
			st.Mutates = true
		}
		return
	}
	method := v.getContent(nameNode)
	if isThisVariable(v, obj) {
		st.SelfCalls = append(st.SelfCalls, method)
		return
	}
	mutator := isMutatorName(method)
	if mutator && passesVariable(v, n.ChildByFieldName("arguments"), s.params) {
		st.MutatesArgs = true
	}
	if propName, ok := v.isPropertyFetchOnThis(obj); ok && isThisVariable(v, obj.ChildByFieldName("object")) {
		// Every call on a property is followed when its class is audited; otherwise only the
		// method name tells whether it changes the object ($this->queue->insert())
		st.DepCalls = append(st.DepCalls, MethodCallRef{Class: v.propertyClass(propName), Method: method, Mutator: mutator})
		return
	}
	if !mutator {
		return
	}
	root := rootVariable(v, obj)
	if s.params[root] {
		st.MutatesArgs = true
	}
	// A mutating call on an object reached from $this through a longer chain or a local
	// variable: the summary cannot tell whose state it is
	if v.derivesFromOwnState(obj) || (root != "" && s.tainted[root]) {
		st.Mutates = true
	}
}

// mutatorPrefixes extend the injected dependency rule's prefixes with the verbs of collections,
// queues, caches and stores: a call the summary cannot read is judged by its name.
var mutatorPrefixes = []string{
	"insert", "put", "attach", "detach", "offsetSet", "offsetUnset", "enqueue", "dequeue",
	"prepend", "unshift", "shift", "pop", "pull", "forget", "flush", "reset", "store", "save",
	"write", "increment", "decrement", "replace", "fill", "delete", "purge", "merge", "splice",
	"transform", "init", "initialize", "configure", "mark", "unset", "toggle", "swap", "extract",
	"sort", "update", "change", "apply", "assign", "bind", "load", "inject", "use", "with",
	"override", "rename", "remember", "cache",
}

// isMutatorName reports a method name that suggests the call changes its object.
func isMutatorName(method string) bool {
	if hasMutationPrefix(method) {
		return true
	}
	lower := strings.ToLower(method)
	for _, prefix := range mutatorPrefixes {
		p := strings.ToLower(prefix)
		if !strings.HasPrefix(lower, p) {
			continue
		}
		if len(method) == len(p) {
			return true
		}
		if next := method[len(p)]; (next >= 'A' && next <= 'Z') || next == '_' {
			return true
		}
	}
	return false
}

func (v *PHPVisitor) summarizeScopedCall(n *sitter.Node, st *MethodState) {
	scope := n.ChildByFieldName("scope")
	nameNode := n.ChildByFieldName("name")
	if scope == nil || nameNode == nil {
		return
	}
	if nameNode.Kind() != "name" {
		st.Mutates = true
		return
	}
	method := v.getContent(nameNode)
	switch strings.ToLower(v.getContent(scope)) {
	case "self", "static":
		st.SelfCalls = append(st.SelfCalls, method)
	case "parent":
		st.ParentCalls = append(st.ParentCalls, method)
	default:
		// Foo::register(...) changes a static registry
		if hasMutationPrefix(method) {
			st.Mutates = true
		}
	}
}

// propertyClass returns the FQCN of a property's declared type, or "" when it is not a
// single class type.
func (v *PHPVisitor) propertyClass(propName string) string {
	t, ok := v.propertyTypes[propName]
	if !ok {
		return ""
	}
	t = strings.TrimPrefix(strings.TrimSpace(t), "?")
	if t == "" || strings.ContainsAny(t, "|&()") {
		return ""
	}
	switch strings.ToLower(t) {
	case "self", "static", "mixed", "object", "array", "iterable", "callable", "string", "int", "float", "bool":
		return ""
	}
	return v.resolveFQCN(t)
}

// writesOwnState reports whether an assignment target (or unset/increment argument) is a
// property of $this, a static property, $GLOBALS, or contains one in a list destructuring.
func (v *PHPVisitor) writesOwnState(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind() {
	case "list_literal", "array_creation_expression", "array_element_initializer", "pair", "by_ref":
		for i := uint(0); i < n.ChildCount(); i++ {
			if v.writesOwnState(n.Child(i)) {
				return true
			}
		}
		return false
	}
	return v.derivesFromOwnState(n)
}

// callWritesOwnState reports a function call that changes state of the object: a by-reference
// builtin given one of its properties (array_push($this->items, ...)), or $this handed to a
// function (call_user_func([$this, 'set']), tap($this, ...)).
func (v *PHPVisitor) callWritesOwnState(n *sitter.Node) bool {
	args := n.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	fn := n.ChildByFieldName("function")
	name := ""
	if fn != nil && (fn.Kind() == "name" || fn.Kind() == "qualified_name") {
		name = strings.ToLower(strings.TrimPrefix(v.getContent(fn), "\\"))
		if i := strings.LastIndex(name, "\\"); i >= 0 {
			name = name[i+1:]
		}
	}
	_, byRef := envByRefFunctions[name]
	for i := uint(0); i < args.ChildCount(); i++ {
		arg := args.Child(i)
		if arg.Kind() != "argument" {
			continue
		}
		if byRef && v.writesOwnState(lastNamedChild(arg)) {
			return true
		}
		if v.passesThis(arg) {
			return true
		}
	}
	return false
}

// passesThis reports an argument that is $this, or an array holding $this.
func (v *PHPVisitor) passesThis(n *sitter.Node) bool {
	switch n.Kind() {
	case "variable_name":
		return v.getContent(n) == "$this"
	case "argument", "array_creation_expression", "array_element_initializer":
		for i := uint(0); i < n.ChildCount(); i++ {
			if v.passesThis(n.Child(i)) {
				return true
			}
		}
	}
	return false
}

func lastNamedChild(n *sitter.Node) *sitter.Node {
	if n.NamedChildCount() == 0 {
		return nil
	}
	return n.NamedChild(n.NamedChildCount() - 1)
}

// derivesFromOwnState reports whether an expression's value is $this, a static property or
// an object or value reached from them (through properties, subscripts and method calls).
func (v *PHPVisitor) derivesFromOwnState(n *sitter.Node) bool {
	for n != nil {
		switch n.Kind() {
		case "member_access_expression", "nullsafe_member_access_expression":
			n = n.ChildByFieldName("object")
		case "member_call_expression", "nullsafe_member_call_expression":
			// find*(), create*(), build*() return a fresh object, as the injected dependency rule assumes
			if name := n.ChildByFieldName("name"); name != nil && isTaintBreakerMethod(v.getContent(name)) {
				return false
			}
			n = n.ChildByFieldName("object")
		case "subscript_expression", "parenthesized_expression":
			if n.ChildCount() == 0 {
				return false
			}
			n = n.Child(0)
			if n.Kind() == "(" && n.NextSibling() != nil {
				n = n.NextSibling()
			}
		case "scoped_property_access_expression":
			return true
		case "scoped_call_expression":
			return v.isOwnStaticCall(n)
		case "variable_name":
			c := v.getContent(n)
			return c == "$this" || c == "$GLOBALS"
		case "conditional_expression", "binary_expression":
			for i := uint(0); i < n.ChildCount(); i++ {
				if v.derivesFromOwnState(n.Child(i)) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
	return false
}

// collectVariables marks every variable bound by a foreach (key, value, list destructuring)
// and reports whether one of them is bound by reference.
func (v *PHPVisitor) collectVariables(foreach *sitter.Node, into map[string]bool) bool {
	byRef := false
	var mark func(*sitter.Node)
	mark = func(x *sitter.Node) {
		switch x.Kind() {
		case "variable_name":
			into[v.getContent(x)] = true
			return
		case "by_ref":
			byRef = true
		}
		for j := uint(0); j < x.ChildCount(); j++ {
			mark(x.Child(j))
		}
	}
	for i := uint(3); i < foreach.ChildCount(); i++ {
		c := foreach.Child(i)
		if c.Kind() == "compound_statement" || c.Kind() == "colon_block" || strings.HasSuffix(c.Kind(), "_statement") {
			break
		}
		mark(c)
	}
	return byRef
}

func isThisVariable(v *PHPVisitor, n *sitter.Node) bool {
	return n != nil && n.Kind() == "variable_name" && v.getContent(n) == "$this"
}

// rootVariable returns the variable at the root of a member/subscript chain.
func rootVariable(v *PHPVisitor, n *sitter.Node) string {
	for n != nil {
		switch n.Kind() {
		case "member_access_expression", "nullsafe_member_access_expression",
			"member_call_expression", "nullsafe_member_call_expression":
			n = n.ChildByFieldName("object")
		case "subscript_expression":
			if n.ChildCount() == 0 {
				return ""
			}
			n = n.Child(0)
		case "variable_name":
			return v.getContent(n)
		default:
			return ""
		}
	}
	return ""
}

// inReturnedClosure reports a node inside a closure that its function returns directly
// (return function () { static $x; ... };). Each call creates a new closure with its own
// static variables, so they do not outlive the closure.
func inReturnedClosure(n *sitter.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "anonymous_function":
			parent := p.Parent()
			return parent != nil && parent.Kind() == "return_statement"
		case "method_declaration", "function_definition", "arrow_function":
			return false
		}
	}
	return false
}

// passesVariable reports an argument list holding one of vars as a whole argument.
func passesVariable(v *PHPVisitor, args *sitter.Node, vars map[string]bool) bool {
	if args == nil {
		return false
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		arg := lastNamedChild(args.NamedChild(i))
		if arg != nil && arg.Kind() == "variable_name" && vars[v.getContent(arg)] {
			return true
		}
	}
	return false
}

// passesSharedState reports a call whose arguments hold the caller's own state.
func (v *PHPVisitor) passesSharedState(call *sitter.Node) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		if arg := lastNamedChild(args.NamedChild(i)); arg != nil && v.isRightSideShared(arg) {
			return true
		}
	}
	return false
}

// isOwnStaticCall reports self::getContainer(), static::instance(): the class's own static state,
// unless the method name says it builds a fresh object (self::createKernel()).
func (v *PHPVisitor) isOwnStaticCall(n *sitter.Node) bool {
	scope, name := n.ChildByFieldName("scope"), n.ChildByFieldName("name")
	if scope == nil || name == nil || isTaintBreakerMethod(v.getContent(name)) {
		return false
	}
	switch strings.ToLower(v.getContent(scope)) {
	case "self", "static", "parent":
		return true
	}
	return false
}
