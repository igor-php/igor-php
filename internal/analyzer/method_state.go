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
	// Self is true for a self:: call.
	Self bool
	// Args says what the call passes: a callee that changes its arguments changes the object's
	// own state when it is given some ($this->index).
	Args CallArgs
}

// CallArgs describes the arguments of a call by position.
type CallArgs struct {
	// Shared lists the positions of arguments holding the caller's own state.
	Shared []int
	// Params gives, for each position, the indexes of the caller's parameters the argument holds:
	// passed as is, reached from there ($holder->index) or wrapped in an array ([$item]).
	Params [][]int
	// Unknown is true when positions cannot be trusted (named or spread arguments).
	Unknown bool
}

// MethodState summarizes whether a method body can change the state of its own
// object or global state. It lets the auditor tell a setter of a stateless service
// (one that only writes to the database) from a real runtime mutation.
type MethodState struct {
	// Mutates is true when the body writes a property of $this, a static property,
	// a global or a static variable, or does something the summary cannot follow.
	Mutates bool
	// MutatesArgs is true when the body changes an object passed as an argument (a mutating call
	// or a property write on a parameter). It matters when the caller passes its own shared state;
	// a parameter handed on to a method the auditor reads is followed through CallArgs.
	MutatesArgs bool
	// MutatedParams are the indexes of the parameters the body changes.
	MutatedParams []int
	// VariadicFrom is the index of a variadic parameter, or -1: it receives every later argument.
	VariadicFrom int
	// SelfCalls are the methods called on $this, self:: or static:: (Class is empty; Self marks
	// self::, which resolves from the class declaring the method, not from the instance).
	SelfCalls []MethodCallRef
	// ParentCalls are the methods called through parent:: (Class is empty).
	ParentCalls []MethodCallRef
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
	// params are the method's parameter names, with their index.
	params map[string]int
	// self are local variables holding $this itself ($self = $this): calls on them are self calls.
	self map[string]bool
	// aliases are local variables holding a parameter, or a collection it was stored in
	// ($items->push($index)), with the indexes of those parameters.
	aliases map[string][]int
	// wrappers are local variables holding a new object built around the object's own state
	// (clone $this->holder, collect([$this->queue])): a change to them is not, a change to what
	// they hold is.
	wrappers map[string]bool
	// sources are local variables holding a property of $this ($cursor = $this->cursor), by name:
	// calls on them are followed as calls on that property.
	sources map[string]string
	// containers are local variables created as a collection (collect(), new \ArrayObject(), []):
	// storing an object in them does not change it.
	containers map[string]bool
}

// summarizeMethodState walks a method declaration. It errs on the side of Mutates: anything
// it cannot follow (dynamic calls, references, objects reached from $this through a local
// variable) counts as a mutation.
func (v *PHPVisitor) summarizeMethodState(method *sitter.Node) MethodState {
	s := &stateSummarizer{
		v: v, tainted: map[string]bool{}, params: map[string]int{}, self: map[string]bool{},
		aliases: map[string][]int{}, containers: map[string]bool{}, wrappers: map[string]bool{},
		sources: map[string]string{},
	}
	s.st.VariadicFrom = -1
	if list := method.ChildByFieldName("parameters"); list != nil {
		index := 0
		for i := uint(0); i < list.NamedChildCount(); i++ {
			param := list.NamedChild(i)
			name := param.ChildByFieldName("name")
			if name == nil {
				continue
			}
			s.params[v.getContent(name)] = index
			if param.Kind() == "variadic_parameter" {
				s.st.VariadicFrom = index
			}
			index++
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
		s.scopedCall(n)
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
		return s.writesTarget(n.ChildByFieldName("argument"))
	case "unset_statement":
		for i := uint(0); i < n.ChildCount(); i++ {
			if s.writesTarget(n.Child(i)) {
				return true
			}
		}
	case "foreach_statement":
		return s.foreach(n)
	case "by_ref":
		// [&$this->prop], fn(&$x) bound to a property
		return n.ChildCount() > 0 && v.derivesFromOwnState(n.Child(n.ChildCount()-1))
	case "function_call_expression":
		return v.callWritesOwnState(n)
	}
	return false
}

// writesTarget reports a write target ($x->count++, unset($x->items[0])) that is the object's own
// state, and records it as a change to the parameter it is rooted at.
func (s *stateSummarizer) writesTarget(target *sitter.Node) bool {
	if s.writesState(target) {
		return true
	}
	if target != nil && target.Kind() != "variable_name" {
		s.mutateParam(rootVariable(s.v, target))
	}
	return false
}

// foreach tracks the loop variables: foreach ($this->items as $item) $item->setX() mutates
// objects the service holds, foreach ($this->items as &$item) writes the property itself, and
// foreach ($items as $item) over a parameter links $item to it.
func (s *stateSummarizer) foreach(n *sitter.Node) bool {
	if n.ChildCount() <= 2 {
		return false
	}
	source := n.Child(2)
	bound := map[string]bool{}
	byRef := s.v.collectVariables(n, bound)
	indexes := s.paramIndexes(source)
	for name := range bound {
		s.alias(name, indexes)
	}
	if !s.holdsOwnState(source) {
		return false
	}
	for name := range bound {
		s.tainted[name] = true
	}
	return byRef
}

func (s *stateSummarizer) assignment(n *sitter.Node) bool {
	v := s.v
	left := n.ChildByFieldName("left")
	if left == nil {
		return false
	}
	if s.writesState(left) {
		return true
	}
	right := n.ChildByFieldName("right")
	if left.Kind() == "variable_name" {
		name := v.getContent(left)
		if right != nil && right.Kind() == "variable_name" && (v.getContent(right) == "$this" || s.self[v.getContent(right)]) {
			s.self[name] = true
		}
		if s.holdsOwnState(right) {
			s.tainted[name] = true
		}
		s.wrappers[name] = v.wrapsSharedState(right, s.holdsOwnState)
		s.sources[name] = ""
		if prop, ok := v.isPropertyFetchOnThis(right); ok && isThisVariable(v, right.ChildByFieldName("object")) {
			s.sources[name] = prop
		}
		// A local reassigned to anything else ($items = $this->queue) is no longer a fresh collection
		s.containers[name] = s.isContainerCreation(right)
		// $local = $index: a change through $local is a change to the parameter
		s.alias(name, s.paramIndexes(right))
	} else {
		s.mutateParam(rootVariable(v, left))
	}
	return false
}

// mutateParam records a change to the parameter named name, or to those a local holds.
func (s *stateSummarizer) mutateParam(name string) {
	if index, ok := s.params[name]; ok {
		s.mutateParamIndex(index)
	}
	for _, index := range s.aliases[name] {
		s.mutateParamIndex(index)
	}
}

// paramIndexes gives the parameters a value holds: a parameter or an object reached from one, or
// a local holding some.
func (s *stateSummarizer) paramIndexes(n *sitter.Node) []int {
	if n == nil {
		return nil
	}
	switch n.Kind() {
	case "variadic_unpacking":
		return s.paramIndexes(lastNamedChild(n))
	case "array_creation_expression", "array_element_initializer", "pair", "parenthesized_expression",
		"conditional_expression", "binary_expression":
		var out []int
		for i := uint(0); i < n.NamedChildCount(); i++ {
			out = append(out, s.paramIndexes(n.NamedChild(i))...)
		}
		return out
	}
	root := s.paramRoot(n)
	if root == "" {
		return nil
	}
	out := append([]int(nil), s.aliases[root]...)
	if index, ok := s.params[root]; ok {
		out = append(out, index)
	}
	return out
}

// alias records that the local name holds the given parameters.
func (s *stateSummarizer) alias(name string, indexes []int) {
	for _, index := range indexes {
		if _, ok := s.params[name]; ok {
			return
		}
		s.aliases[name] = append(s.aliases[name], index)
	}
}

// containerClasses are the classes whose instances only store what they are given.
var containerClasses = map[string]bool{
	"arrayobject": true, "splobjectstorage": true, "splqueue": true, "splstack": true,
	"spldoublylinkedlist": true, "splfixedarray": true, "arraycollection": true, "collection": true,
}

// isContainerCreation reports a value that is a new collection: collect(...), an array literal,
// or a new \ArrayObject, \SplObjectStorage or collection class.
func (s *stateSummarizer) isContainerCreation(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind() {
	case "array_creation_expression":
		return true
	case "function_call_expression":
		fn := n.ChildByFieldName("function")
		return fn != nil && strings.EqualFold(strings.TrimPrefix(s.v.getContent(fn), "\\"), "collect")
	case "object_creation_expression":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			if c.Kind() != "name" && c.Kind() != "qualified_name" {
				continue
			}
			class := strings.ToLower(s.v.getContent(c))
			if j := strings.LastIndex(class, "\\"); j >= 0 {
				class = class[j+1:]
			}
			return containerClasses[class] || strings.HasSuffix(class, "collection")
		}
	}
	return false
}

// mutateParamIndex records a change to the parameter at index.
func (s *stateSummarizer) mutateParamIndex(index int) {
	s.st.MutatesArgs = true
	for _, i := range s.st.MutatedParams {
		if i == index {
			return
		}
	}
	s.st.MutatedParams = append(s.st.MutatedParams, index)
}

// writesState reports a write target that is the object's own state, directly or through a
// local variable holding it ($self = $this; $self->value = 1).
func (s *stateSummarizer) writesState(target *sitter.Node) bool {
	if s.v.writesOwnState(target) {
		return true
	}
	if target == nil || target.Kind() == "variable_name" {
		return false
	}
	root := rootVariable(s.v, target)
	return root != "" && (s.tainted[root] || s.self[root])
}

// holdsOwnState reports a value that is, or holds, the object's own state: $this->items, a
// local already holding it, or an array of these ([$this->queue]).
func (s *stateSummarizer) holdsOwnState(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind() {
	case "array_creation_expression", "array_element_initializer", "pair", "parenthesized_expression",
		"conditional_expression", "binary_expression":
		for i := uint(0); i < n.ChildCount(); i++ {
			if s.holdsOwnState(n.Child(i)) {
				return true
			}
		}
		return false
	}
	if s.v.derivesFromOwnState(n) || s.v.returnsArgument(n, s.holdsOwnState) {
		return true
	}
	root := rootVariable(s.v, n)
	// A wrapper is fresh, but what is reached through it ($copy->queue) may be shared
	return root != "" && (s.tainted[root] || s.self[root] || (s.wrappers[root] && n.Kind() != "variable_name"))
}

// callArgs describes the arguments of a call: which hold the object's own state, and which
// pass one of the method's parameters.
func (s *stateSummarizer) callArgs(call *sitter.Node) CallArgs {
	var out CallArgs
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return out
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		arg := args.NamedChild(i)
		value := lastNamedChild(arg)
		if arg.ChildByFieldName("name") != nil || arg.Kind() == "variadic_unpacking" || (value != nil && value.Kind() == "variadic_unpacking") {
			out.Unknown = true
		}
		pos := int(i)
		if s.holdsOwnState(value) {
			out.Shared = append(out.Shared, pos)
		}
		// A parameter, or an object reached from one ($holder->index): a change made there by
		// the callee is a change to what the caller was given
		out.Params = append(out.Params, s.paramIndexes(value))
	}
	return out
}

// paramRoot returns the variable at the root of an argument, like rootVariable, or "" when the
// chain goes through a call that returns a fresh object ($factory->createIndex()).
func (s *stateSummarizer) paramRoot(n *sitter.Node) string {
	for x := n; x != nil; x = x.ChildByFieldName("object") {
		switch x.Kind() {
		case "member_call_expression", "nullsafe_member_call_expression":
			if name := x.ChildByFieldName("name"); name != nil && isTaintBreakerMethod(s.v.getContent(name)) {
				return ""
			}
		case "member_access_expression", "nullsafe_member_access_expression":
		default:
			return rootVariable(s.v, n)
		}
	}
	return rootVariable(s.v, n)
}

// isSelf reports $this or a local variable holding it.
func (s *stateSummarizer) isSelf(n *sitter.Node) bool {
	return isThisVariable(s.v, n) || (n != nil && n.Kind() == "variable_name" && s.self[s.v.getContent(n)])
}

func (s *stateSummarizer) memberCall(n *sitter.Node) {
	v, st := s.v, &s.st
	obj := n.ChildByFieldName("object")
	nameNode := n.ChildByFieldName("name")
	if obj == nil || nameNode == nil {
		return
	}
	if nameNode.Kind() != "name" {
		// $this->{$method}(), $this->options->{$method}(): any method, setters included
		if s.isSelf(obj) || s.holdsOwnState(obj) {
			st.Mutates = true
		}
		return
	}
	method := v.getContent(nameNode)
	if s.isSelf(obj) {
		st.SelfCalls = append(st.SelfCalls, MethodCallRef{Method: method, Args: s.callArgs(n)})
		return
	}
	mutator := isMutatorName(method)
	args := s.callArgs(n)
	propName, ok := v.isPropertyFetchOnThis(obj)
	ok = ok && isThisVariable(v, obj.ChildByFieldName("object"))
	if !ok && obj.Kind() == "variable_name" && s.sources[v.getContent(obj)] != "" {
		// $cursor = $this->cursor; $cursor->advance(): followed as $this->cursor->advance()
		propName, ok = s.sources[v.getContent(obj)], true
	}
	if ok {
		// Every call on a property is followed when its class is audited; otherwise only the
		// method name tells whether it changes the object ($this->queue->insert())
		st.DepCalls = append(st.DepCalls, MethodCallRef{
			Class:   v.propertyClass(propName),
			Method:  method,
			Mutator: mutator,
			Args:    args,
		})
		return
	}
	if mutator {
		s.localCall(n, obj, method, args)
	}
}

// localCall handles a mutator-named call on a receiver the summary does not follow: a
// parameter, a local variable or a longer chain.
func (s *stateSummarizer) localCall(call, obj *sitter.Node, method string, args CallArgs) {
	v, st := s.v, &s.st
	root := rootVariable(v, obj)
	_, onParam := s.params[root]
	onParam = onParam || len(s.aliases[root]) > 0
	if s.containers[root] && obj.Kind() == "variable_name" && storageMethods[strings.ToLower(method)] {
		// $items = collect([]); $items->push($index): storing an object does not change it, but
		// the collection now holds what it was given, and a change through it counts
		if len(args.Shared) > 0 {
			s.tainted[root] = true
		}
		s.storeArgs(call, root)
		return
	}
	s.mutateParam(root)
	// A mutating call on an object reached from $this through a longer chain or a local
	// variable, or a local given the object's own state ($helper->rename($this->index)): the
	// summary cannot tell whose state it changes. A helper's result (session()->forget($this->key))
	// is left to its name: it is not an object of the method
	if s.holdsOwnState(obj) || (root != "" && len(args.Shared) > 0) {
		st.Mutates = true
		return
	}
	// A local receiver the auditor does not follow ($helper = new Helper()) may change what it is
	// given: count the parameters passed to it as changed. On a parameter ($playlist->addSongs($user))
	// the change is the receiver's, counted above
	if onParam {
		return
	}
	for _, params := range args.Params {
		for _, param := range params {
			s.mutateParamIndex(param)
		}
	}
}

// storeArgs links a collection to the parameters stored in it.
func (s *stateSummarizer) storeArgs(call *sitter.Node, root string) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		s.alias(root, s.paramIndexes(lastNamedChild(args.NamedChild(i))))
	}
}

// storageMethods put their arguments into the receiver without changing them.
var storageMethods = map[string]bool{
	"push": true, "add": true, "append": true, "prepend": true, "put": true, "insert": true,
	"attach": true, "offsetset": true, "enqueue": true, "unshift": true, "merge": true,
	"concat": true, "set": true,
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

func (s *stateSummarizer) scopedCall(n *sitter.Node) {
	v, st := s.v, &s.st
	scope := n.ChildByFieldName("scope")
	nameNode := n.ChildByFieldName("name")
	if scope == nil || nameNode == nil {
		return
	}
	if nameNode.Kind() != "name" {
		st.Mutates = true
		return
	}
	call := MethodCallRef{Method: v.getContent(nameNode), Args: s.callArgs(n)}
	switch strings.ToLower(v.getContent(scope)) {
	case "self":
		call.Self = true
		st.SelfCalls = append(st.SelfCalls, call)
	case "static":
		st.SelfCalls = append(st.SelfCalls, call)
	case "parent":
		st.ParentCalls = append(st.ParentCalls, call)
	default:
		// Foo::register(...), Registry::flush(): a static registry the summary does not read
		if isMutatorName(call.Method) {
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
// property of $this, a static property, a superglobal, or contains one in a list destructuring.
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
	if superglobals[rootVariable(v, n)] {
		// $_SESSION['user'] = $user, $_ENV['TENANT'] = $tenant: process-wide state
		return true
	}
	return v.derivesFromOwnState(n)
}

// superglobals are written as global state; reading them is left to the superglobal rule.
var superglobals = map[string]bool{
	"$GLOBALS": true, "$_SERVER": true, "$_GET": true, "$_POST": true, "$_FILES": true,
	"$_COOKIE": true, "$_SESSION": true, "$_REQUEST": true, "$_ENV": true,
}

// callWritesOwnState reports a function call that changes state of the object or of the process:
// a function igor reports as changing the process (ini_set(), date_default_timezone_set()), a
// by-reference builtin given one of its properties (array_push($this->items, ...)), or $this
// handed to a function (call_user_func([$this, 'set']), tap($this, ...)).
func (v *PHPVisitor) callWritesOwnState(n *sitter.Node) bool {
	fn := n.ChildByFieldName("function")
	name := ""
	if fn != nil && (fn.Kind() == "name" || fn.Kind() == "qualified_name") {
		raw := v.getContent(fn)
		// Matched as the process state rule matches it: shadowing and getter calls included
		if _, ok := v.processStateRule(n, strings.ToLower(strings.TrimPrefix(raw, "\\")), strings.HasPrefix(raw, "\\")); ok {
			return true
		}
		name = strings.ToLower(raw)
		if i := strings.LastIndex(name, "\\"); i >= 0 {
			name = name[i+1:]
		}
	}
	args := n.ChildByFieldName("arguments")
	if args == nil {
		return false
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

// passesSharedState reports a call whose arguments hold the caller's own state.
func (v *PHPVisitor) passesSharedState(call *sitter.Node) bool {
	positions, unknown := v.sharedArgPositions(call)
	return len(positions) > 0 || unknown
}

// sharedArgPositions lists the positions of a call's arguments that hold the caller's own state,
// and reports named or spread arguments, whose positions cannot be trusted.
func (v *PHPVisitor) sharedArgPositions(call *sitter.Node) ([]int, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return nil, false
	}
	var positions []int
	unknown := false
	for i := uint(0); i < args.NamedChildCount(); i++ {
		arg := args.NamedChild(i)
		value := lastNamedChild(arg)
		if value == nil {
			continue
		}
		if !v.isRightSideShared(value) {
			continue
		}
		positions = append(positions, int(i))
		if arg.ChildByFieldName("name") != nil || arg.Kind() == "variadic_unpacking" || value.Kind() == "variadic_unpacking" {
			unknown = true
		}
	}
	return positions, unknown
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

// freshBuilders are functions whose result is a new value built from their arguments, or a scalar:
// it may hold the objects it was given, but it is not one of them.
var freshBuilders = map[string]bool{
	"collect": true, "array_map": true, "array_filter": true, "array_merge": true, "array_values": true,
	"array_keys": true, "array_slice": true, "array_combine": true, "array_reverse": true,
	"array_unique": true, "array_column": true, "array_fill_keys": true, "iterator_to_array": true,
	"compact": true, "count": true, "sprintf": true, "implode": true, "json_encode": true,
	"serialize": true, "in_array": true, "array_key_exists": true, "is_array": true, "get_class": true,
	"spl_object_id": true, "spl_object_hash": true, "md5": true, "sha1": true, "strtolower": true,
	"strtoupper": true, "trim": true, "str_replace": true, "array_sum": true, "max": true, "min": true,
}

// functionName returns the lowercased name of a called function, without its namespace.
func (v *PHPVisitor) functionName(call *sitter.Node) string {
	fn := call.ChildByFieldName("function")
	if fn == nil || (fn.Kind() != "name" && fn.Kind() != "qualified_name") {
		return ""
	}
	name := strings.ToLower(v.getContent(fn))
	if i := strings.LastIndex(name, "\\"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// anyArgument reports a call whose arguments hold a value matching shared.
func anyArgument(call *sitter.Node, shared func(*sitter.Node) bool) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		if shared(lastNamedChild(args.NamedChild(i))) {
			return true
		}
	}
	return false
}

// returnsArgument reports a function call that may hand back an object it was given
// (current($this->queues), reset(), a helper): any function but the known builders.
func (v *PHPVisitor) returnsArgument(n *sitter.Node, shared func(*sitter.Node) bool) bool {
	if n == nil || n.Kind() != "function_call_expression" {
		return false
	}
	name := v.functionName(n)
	return name != "" && !freshBuilders[name] && anyArgument(n, shared)
}

// wrapsSharedState reports a new value built around shared state: clone $this->holder,
// new Wrapper($this->queue), collect([$this->queue]). The value is fresh, the objects it holds
// may not be.
func (v *PHPVisitor) wrapsSharedState(n *sitter.Node, shared func(*sitter.Node) bool) bool {
	if n == nil {
		return false
	}
	switch n.Kind() {
	case "clone_expression":
		return n.NamedChildCount() > 0 && shared(lastNamedChild(n))
	case "object_creation_expression":
		return anyArgument(n, shared)
	case "function_call_expression":
		return freshBuilders[v.functionName(n)] && anyArgument(n, shared)
	}
	return false
}
