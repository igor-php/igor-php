package analyzer

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// ForwardKind says on what a method hands one of its parameters to another method.
type ForwardKind int

const (
	// ForwardSelf is a call on $this, self:: or static::.
	ForwardSelf ForwardKind = iota
	// ForwardParent is a parent:: call.
	ForwardParent
	// ForwardDependency is a call on a property of $this.
	ForwardDependency
	// ForwardStatic is a static call on another class.
	ForwardStatic
)

// ParamForward is a parameter passed on as the argument at Pos of another method.
type ParamForward struct {
	Kind ForwardKind
	// Class is the class of the property (ForwardDependency) or of the static call (ForwardStatic).
	Class  string
	Method string
	Pos    int
	// Self is true for self::, which resolves from the class declaring the method.
	Self bool
}

// ParamFate says what a method does with the value it receives in one parameter: whether that
// value outlives the call. It answers one question for the closure rule: does the method keep
// the closure it is given, or only call it?
type ParamFate struct {
	// Retained is true when the value is stored in the object's own state, in static or global
	// state, or registered with PHP itself (register_shutdown_function()).
	Retained bool
	// Unknown is true when the value goes somewhere the summary cannot follow.
	Unknown bool
	// Forwards are the methods the value is passed on to: it is kept if one of them keeps it.
	Forwards []ParamForward
}

// MethodCallables is the fate of each parameter of a method.
type MethodCallables struct {
	Params []ParamFate
	// VariadicFrom is the index of a variadic parameter, or -1.
	VariadicFrom int
	// HasBody is false for an abstract or interface method.
	HasBody bool
	// Opaque is true when the body reads its arguments in a way the summary cannot follow
	// (func_get_args(), $$name): even an argument without a parameter may be kept.
	Opaque bool
}

// ClassCallables describes a class, interface or trait for the closure rule.
type ClassCallables struct {
	// Concrete is true for a class that can be instantiated: not abstract, not an interface or trait.
	Concrete bool
	Parent   string
	Traits   []string
	// Methods is keyed by lowercased method name.
	Methods map[string]MethodCallables
}

// SummarizeCallables parses a PHP file and returns, for every class, interface and trait it
// declares (by FQCN), what its methods do with their parameters.
func SummarizeCallables(content []byte) map[string]ClassCallables {
	p := sitter.NewParser()
	defer p.Close()
	if err := p.SetLanguage(sitter.NewLanguage(php.LanguagePHP())); err != nil {
		return nil
	}
	tree := p.Parse(content, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	out := map[string]ClassCallables{}
	var walk func(n *sitter.Node, namespace string)
	walk = func(n *sitter.Node, namespace string) {
		for i := uint(0); i < n.ChildCount(); i++ {
			c := n.Child(i)
			switch c.Kind() {
			case "namespace_definition":
				name := ""
				if nameNode := c.ChildByFieldName("name"); nameNode != nil {
					name = string(content[nameNode.StartByte():nameNode.EndByte()])
				}
				if body := c.ChildByFieldName("body"); body != nil {
					walk(body, name)
				} else {
					namespace = name
				}
			case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration":
				nameNode := c.ChildByFieldName("name")
				if nameNode == nil {
					continue
				}
				// A fresh visitor per class: it resolves names against the file's imports and
				// holds the property types of this class only
				v := NewVisitor(content, nil)
				v.namespace = namespace
				fqcn := v.getContent(nameNode)
				if namespace != "" {
					fqcn = namespace + "\\" + fqcn
				}
				out[fqcn] = v.classCallables(c)
			case "if_statement", "compound_statement", "declaration_list":
				// class_exists() guards and bracketed blocks
				walk(c, namespace)
			}
		}
	}
	walk(tree.RootNode(), "")
	return out
}

func (v *PHPVisitor) classCallables(class *sitter.Node) ClassCallables {
	out := ClassCallables{Concrete: class.Kind() == "class_declaration", Methods: map[string]MethodCallables{}}
	for i := uint(0); i < class.ChildCount(); i++ {
		c := class.Child(i)
		switch c.Kind() {
		case "abstract_modifier":
			out.Concrete = false
		case "base_clause":
			if class.Kind() == "class_declaration" && c.NamedChildCount() > 0 {
				out.Parent = v.resolveFQCN(v.getContent(c.NamedChild(0)))
			}
		}
	}
	body := v.getClassBody(class)
	if body == nil {
		return out
	}
	v.scanPropertyTypes(class)
	for i := uint(0); i < body.ChildCount(); i++ {
		member := body.Child(i)
		switch member.Kind() {
		case "use_declaration":
			for j := uint(0); j < member.NamedChildCount(); j++ {
				if t := member.NamedChild(j); t.Kind() == "name" || t.Kind() == "qualified_name" {
					out.Traits = append(out.Traits, v.resolveTraitName(class, v.getContent(t)))
				}
			}
		case "method_declaration":
			if nameNode := member.ChildByFieldName("name"); nameNode != nil {
				out.Methods[strings.ToLower(v.getContent(nameNode))] = v.methodCallables(member)
			}
		}
	}
	return out
}

// resolveTraitName resolves the name of a trait used by class. The imports are matched by line,
// and "use Deferring;" in a class body reads like one: it only counts when the file really
// imports that name.
func (v *PHPVisitor) resolveTraitName(class *sitter.Node, name string) string {
	fqcn := v.resolveFQCN(name)
	if fqcn != name || v.namespace == "" || strings.Contains(name, "\\") {
		return fqcn
	}
	root := class
	for root.Parent() != nil {
		root = root.Parent()
	}
	if v.importsName(root, name) {
		return fqcn
	}
	return v.namespace + "\\" + name
}

// importsName reports a namespace import (use Foo; use Bar as Foo;) of name under n.
func (v *PHPVisitor) importsName(n *sitter.Node, name string) bool {
	for i := uint(0); i < n.ChildCount(); i++ {
		c := n.Child(i)
		switch c.Kind() {
		case "namespace_use_declaration":
			text := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(v.getContent(c), "use")), ";")
			for _, clause := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '{' || r == '}' }) {
				clause = strings.TrimSpace(clause)
				if _, alias, ok := strings.Cut(clause, " as "); ok {
					clause = strings.TrimSpace(alias)
				}
				if clause[strings.LastIndex(clause, "\\")+1:] == name {
					return true
				}
			}
		case "namespace_definition", "compound_statement", "declaration_list":
			if v.importsName(c, name) {
				return true
			}
		}
	}
	return false
}

// fateScanner follows the parameters of one method through its body.
type fateScanner struct {
	v      *PHPVisitor
	params map[string]int
	// holders are local variables holding a parameter, or something built around it (an array,
	// a closure capturing it, an object created with it), with the indexes of those parameters.
	holders map[string]map[int]bool
	fates   []ParamFate
	grew    bool
	opaque  bool
}

func (v *PHPVisitor) methodCallables(method *sitter.Node) MethodCallables {
	out := MethodCallables{VariadicFrom: -1}
	s := &fateScanner{v: v, params: map[string]int{}, holders: map[string]map[int]bool{}}
	var promoted, byRef []int
	if list := method.ChildByFieldName("parameters"); list != nil {
		index := 0
		for i := uint(0); i < list.NamedChildCount(); i++ {
			param := list.NamedChild(i)
			name := param.ChildByFieldName("name")
			if name == nil {
				continue
			}
			s.params[v.getContent(name)] = index
			switch param.Kind() {
			case "variadic_parameter":
				out.VariadicFrom = index
			case "property_promotion_parameter":
				promoted = append(promoted, index)
			}
			if param.ChildByFieldName("reference_modifier") != nil {
				byRef = append(byRef, index)
			}
			index++
		}
	}
	count := len(s.params)
	body := method.ChildByFieldName("body")
	if body == nil {
		out.Params = make([]ParamFate, count)
		return out
	}
	out.HasBody = true

	// A local may be given a parameter after a line that uses it (in a loop): scan until the
	// set of locals holding a parameter stops growing
	for round := 0; round < 8; round++ {
		s.fates = make([]ParamFate, count)
		s.grew = false
		s.scan(body)
		if !s.grew {
			break
		}
	}
	for _, i := range promoted {
		// public function __construct(private \Closure $callback): stored as a property
		s.fates[i].Retained = true
	}
	for _, i := range byRef {
		s.fates[i].Unknown = true
	}
	out.Params = s.fates
	out.Opaque = s.opaque
	return out
}

// opaqueFunctions read every local variable of the method: nothing can be said about them.
var opaqueFunctions = map[string]bool{
	"func_get_args": true, "func_get_arg": true, "get_defined_vars": true, "compact": true, "extract": true,
}

func (s *fateScanner) scan(n *sitter.Node) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "class_declaration", "anonymous_class":
		// Its methods have their own variables
		return
	case "variable_name":
		if p := n.Parent(); p != nil && p.Kind() == "dynamic_variable_name" {
			s.allUnknown()
		}
		if indexes := s.held(s.v.getContent(n)); len(indexes) > 0 && !s.isBinding(n) {
			s.follow(n, indexes)
		}
		return
	case "dynamic_variable_name":
		// $$name: any local
		s.allUnknown()
	case "function_call_expression":
		if opaqueFunctions[s.v.functionName(n)] {
			s.allUnknown()
		}
	case "arrow_function":
		// fn () => $callback(): the arrow function holds every variable its body uses
		if indexes := s.usedIn(n.ChildByFieldName("body")); len(indexes) > 0 {
			s.follow(n, indexes)
		}
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		s.scan(n.Child(i))
	}
}

// isBinding reports a variable that is being declared, not read: a parameter of a nested
// function, or the target of a plain assignment.
func (s *fateScanner) isBinding(n *sitter.Node) bool {
	p := n.Parent()
	if p == nil {
		return false
	}
	switch p.Kind() {
	case "simple_parameter", "variadic_parameter", "property_promotion_parameter":
		return true
	case "assignment_expression":
		left := p.ChildByFieldName("left")
		return left != nil && left.StartByte() == n.StartByte() && left.EndByte() == n.EndByte()
	}
	return false
}

func (s *fateScanner) allUnknown() {
	s.opaque = true
	for i := range s.fates {
		s.fates[i].Unknown = true
	}
}

// held gives the parameters a variable holds.
func (s *fateScanner) held(name string) []int {
	var out []int
	if index, ok := s.params[name]; ok {
		out = append(out, index)
	}
	for index := range s.holders[name] {
		out = append(out, index)
	}
	return out
}

// usedIn gives the parameters held by the variables used anywhere under n.
func (s *fateScanner) usedIn(n *sitter.Node) []int {
	seen := map[int]bool{}
	var visit func(*sitter.Node)
	visit = func(x *sitter.Node) {
		if x == nil {
			return
		}
		if x.Kind() == "variable_name" {
			for _, index := range s.held(s.v.getContent(x)) {
				seen[index] = true
			}
			return
		}
		for i := uint(0); i < x.ChildCount(); i++ {
			visit(x.Child(i))
		}
	}
	visit(n)
	out := make([]int, 0, len(seen))
	for index := range seen {
		out = append(out, index)
	}
	return out
}

func (s *fateScanner) hold(name string, indexes []int) {
	if name == "" || name == "$this" {
		return
	}
	if _, isParam := s.params[name]; isParam {
		return
	}
	if s.holders[name] == nil {
		s.holders[name] = map[int]bool{}
	}
	for _, index := range indexes {
		if !s.holders[name][index] {
			s.holders[name][index] = true
			s.grew = true
		}
	}
}

func (s *fateScanner) retain(indexes []int) {
	for _, i := range indexes {
		s.fates[i].Retained = true
	}
}

func (s *fateScanner) unknown(indexes []int) {
	for _, i := range indexes {
		s.fates[i].Unknown = true
	}
}

func (s *fateScanner) forward(indexes []int, f ParamForward) {
	for _, i := range indexes {
		s.fates[i].Forwards = append(s.fates[i].Forwards, f)
	}
}

// wrapperKinds are the nodes whose value still holds what they wrap: an array, a closure capturing
// it, a cast, a branch of a ternary or a match.
var wrapperKinds = map[string]bool{
	"parenthesized_expression": true, "array_creation_expression": true, "array_element_initializer": true,
	"conditional_expression": true, "cast_expression": true, "clone_expression": true,
	"anonymous_function_use_clause": true, "anonymous_function": true, "by_ref": true,
	"variadic_unpacking": true, "match_expression": true, "match_conditional_expression": true,
	"match_default_expression": true, "match_block": true, "return_expression": true,
}

// follow climbs from a value holding parameters to what the method does with it. Wrapping the
// value (an array, a closure capturing it, a new object) gives a value that still holds it.
func (s *fateScanner) follow(value *sitter.Node, indexes []int) {
	for cur := value; cur != nil; {
		p := cur.Parent()
		if p == nil {
			return
		}
		if wrapperKinds[p.Kind()] {
			cur = p
			continue
		}
		cur = s.step(p, cur, indexes)
	}
}

// step handles the parent p of a value cur holding parameters. It returns the node whose value
// still holds them, nil when the value ends there.
func (s *fateScanner) step(p, cur *sitter.Node, indexes []int) *sitter.Node {
	if s.stillHolds(p, cur) {
		return p
	}
	switch p.Kind() {
	case "argument":
		return s.argument(p, indexes)
	case "assignment_expression":
		if sameNode(p.ChildByFieldName("right"), cur) {
			s.assign(p.ChildByFieldName("left"), indexes)
		}
	case "reference_assignment_expression", "global_declaration":
		s.unknown(indexes)
	case "static_variable_declaration":
		s.retain(indexes)
	case "foreach_statement":
		// foreach ($listeners as $listener): the loop variables hold what the source holds
		if p.NamedChildCount() > 0 && sameNode(p.NamedChild(0), cur) {
			bound := map[string]bool{}
			s.v.collectVariables(p, bound)
			for name := range bound {
				s.hold(name, indexes)
			}
		}
	}
	// Returned, yielded, compared, tested, printed: the method does not keep it
	return nil
}

// stillHolds reports an expression p whose value holds what its operand cur holds.
func (s *fateScanner) stillHolds(p, cur *sitter.Node) bool {
	v := s.v
	switch p.Kind() {
	case "pair":
		// A foreach binding ($key => $value) is not a value
		gp := p.Parent()
		return gp == nil || gp.Kind() != "foreach_statement"
	case "binary_expression":
		// $callback ?? $default and $callback ?: $default give the value back; a comparison does not
		op := p.ChildByFieldName("operator")
		return op != nil && (v.getContent(op) == "??" || v.getContent(op) == "?:")
	case "subscript_expression":
		// $listeners[0] is one of the values held; $map[$callback] only reads it
		return p.ChildCount() > 0 && sameNode(p.Child(0), cur)
	case "function_call_expression":
		// $callback(...) creates a closure around it; $callback() only calls it
		return sameNode(p.ChildByFieldName("function"), cur) && hasVariadicPlaceholder(p)
	case "member_call_expression", "nullsafe_member_call_expression":
		// A call on the value itself: bindTo() gives a new closure holding it, the others use it
		name := p.ChildByFieldName("name")
		return sameNode(p.ChildByFieldName("object"), cur) && name != nil && strings.EqualFold(v.getContent(name), "bindTo")
	}
	return false
}

func hasVariadicPlaceholder(call *sitter.Node) bool {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return false
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		if args.NamedChild(i).Kind() == "variadic_placeholder" {
			return true
		}
	}
	return false
}

// assign records a value stored in left: a local now holds it, the object's own state or static
// state keeps it.
func (s *fateScanner) assign(left *sitter.Node, indexes []int) {
	v := s.v
	if left == nil {
		return
	}
	switch left.Kind() {
	case "variable_name":
		s.hold(v.getContent(left), indexes)
		return
	case "list_literal", "array_creation_expression":
		bound := map[string]bool{}
		var mark func(*sitter.Node)
		mark = func(x *sitter.Node) {
			if x.Kind() == "variable_name" {
				bound[v.getContent(x)] = true
				return
			}
			for i := uint(0); i < x.ChildCount(); i++ {
				mark(x.Child(i))
			}
		}
		mark(left)
		for name := range bound {
			s.hold(name, indexes)
		}
		return
	}
	if v.writesOwnState(left) {
		s.retain(indexes)
		return
	}
	root := rootVariable(v, left)
	if root == "$this" {
		s.retain(indexes)
		return
	}
	if _, isParam := s.params[root]; root == "" || isParam || len(s.holders[root]) > 0 && left.Kind() != "subscript_expression" {
		// Stored in an object the caller passed, or one the summary lost track of
		s.unknown(indexes)
		return
	}
	// $listeners[] = $callback, $wrapper->callback = $callback on a local
	s.hold(root, indexes)
}

// callableInvokers are PHP functions that call or inspect the callable they are given, and
// return without keeping it.
var callableInvokers = map[string]bool{
	"call_user_func": true, "call_user_func_array": true, "array_map": true, "array_filter": true,
	"array_walk": true, "array_walk_recursive": true, "array_reduce": true, "usort": true, "uasort": true,
	"uksort": true, "array_udiff": true, "array_uintersect": true, "array_find": true, "array_any": true,
	"array_all": true, "iterator_apply": true, "preg_replace_callback": true, "is_callable": true,
	"is_null": true, "is_array": true, "is_object": true, "is_string": true, "is_iterable": true,
	"count": true, "in_array": true, "array_key_exists": true, "array_keys": true, "get_class": true,
	"gettype": true, "get_debug_type": true, "spl_object_id": true, "spl_object_hash": true,
	"method_exists": true, "function_exists": true, "var_dump": true, "var_export": true, "sprintf": true,
	"assert": true, "iterator_count": true, "is_a": true, "is_subclass_of": true, "array_key_first": true,
	"array_key_last": true, "empty": true, "isset": true,
}

// callableHolders are PHP functions whose result still holds the values they were given.
var callableHolders = map[string]bool{
	"array_merge": true, "array_values": true, "array_slice": true, "array_reverse": true,
	"array_replace": true, "array_pad": true, "array_combine": true, "array_fill": true,
	"array_fill_keys": true, "array_unique": true, "iterator_to_array": true, "array_shift": true,
	"array_pop": true, "current": true, "reset": true, "end": true,
}

// callableRegistrars are PHP functions that keep the callable for the life of the process.
var callableRegistrars = map[string]bool{
	"register_shutdown_function": true, "spl_autoload_register": true, "set_error_handler": true,
	"set_exception_handler": true, "ob_start": true, "header_register_callback": true,
	"register_tick_function": true, "pcntl_signal": true, "pcntl_async_signals": true,
	"stream_filter_register": true, "session_set_save_handler": true, "assert_options": true,
}

// byRefStores are PHP functions that put their other arguments into their first one.
var byRefStores = map[string]bool{"array_push": true, "array_unshift": true, "array_splice": true}

// argument handles a value passed as an argument. It returns the call when its result still
// holds the value (new Wrapper($callback), array_merge($listeners, [$callback])), nil otherwise.
func (s *fateScanner) argument(arg *sitter.Node, indexes []int) *sitter.Node {
	args := arg.Parent()
	if args == nil || args.Parent() == nil {
		return nil
	}
	call := args.Parent()
	pos, positional := argumentPosition(args, arg)

	switch call.Kind() {
	case "object_creation_expression":
		return call
	case "function_call_expression":
		return s.functionArgument(call, args, pos, indexes)
	case "member_call_expression", "nullsafe_member_call_expression":
		s.methodArgument(call, pos, positional, indexes)
		return nil
	case "scoped_call_expression":
		return s.staticArgument(call, pos, positional, indexes)
	}
	s.unknown(indexes)
	return nil
}

// argumentPosition gives the position of arg in args. positional is false when it, or an
// argument before it, is named or spread: the position does not name a parameter.
func argumentPosition(args, arg *sitter.Node) (pos int, positional bool) {
	unordered := func(x *sitter.Node) bool {
		value := lastNamedChild(x)
		return x.ChildByFieldName("name") != nil || x.Kind() == "variadic_unpacking" || (value != nil && value.Kind() == "variadic_unpacking")
	}
	positional = true
	for i := uint(0); i < args.NamedChildCount(); i++ {
		x := args.NamedChild(i)
		if unordered(x) {
			positional = false
		}
		if sameNode(x, arg) {
			return int(i), positional
		}
	}
	return 0, false
}

// functionArgument handles a value passed to a function.
func (s *fateScanner) functionArgument(call, args *sitter.Node, pos int, indexes []int) *sitter.Node {
	name := s.v.functionName(call)
	switch {
	case callableInvokers[name]:
	case callableHolders[name]:
		return call
	case callableRegistrars[name]:
		s.retain(indexes)
	case byRefStores[name] && pos > 0:
		if first := args.NamedChild(0); first != nil {
			s.assign(lastNamedChild(first), indexes)
		}
	default:
		s.unknown(indexes)
	}
	return nil
}

// methodArgument handles a value passed to a method: one of the class ($this->keep($callback)),
// or one of a dependency ($this->registry->add($callback)).
func (s *fateScanner) methodArgument(call *sitter.Node, pos int, positional bool, indexes []int) {
	v := s.v
	nameNode, obj := call.ChildByFieldName("name"), call.ChildByFieldName("object")
	if nameNode == nil || nameNode.Kind() != "name" || obj == nil || !positional {
		s.unknown(indexes)
		return
	}
	method := v.getContent(nameNode)
	if isThisVariable(v, obj) {
		s.forward(indexes, ParamForward{Kind: ForwardSelf, Method: method, Pos: pos})
		return
	}
	if prop, ok := v.isPropertyFetchOnThis(obj); ok && isThisVariable(v, obj.ChildByFieldName("object")) {
		if class := v.propertyClass(prop); class != "" {
			s.forward(indexes, ParamForward{Kind: ForwardDependency, Class: class, Method: method, Pos: pos})
			return
		}
	}
	s.unknown(indexes)
}

// staticArgument handles a value passed to a static call. Closure::fromCallable($callback) and
// Closure::bind($callback, ...) give a closure around it: the call is returned.
func (s *fateScanner) staticArgument(call *sitter.Node, pos int, positional bool, indexes []int) *sitter.Node {
	v := s.v
	nameNode, scope := call.ChildByFieldName("name"), call.ChildByFieldName("scope")
	if nameNode == nil || nameNode.Kind() != "name" || scope == nil {
		s.unknown(indexes)
		return nil
	}
	method, scopeName := v.getContent(nameNode), v.getContent(scope)
	if strings.EqualFold(strings.TrimPrefix(scopeName, "\\"), "Closure") {
		return call
	}
	if !positional {
		s.unknown(indexes)
		return nil
	}
	switch strings.ToLower(scopeName) {
	case "self":
		s.forward(indexes, ParamForward{Kind: ForwardSelf, Method: method, Pos: pos, Self: true})
	case "static":
		s.forward(indexes, ParamForward{Kind: ForwardSelf, Method: method, Pos: pos})
	case "parent":
		s.forward(indexes, ParamForward{Kind: ForwardParent, Method: method, Pos: pos})
	default:
		if scope.Kind() != "name" && scope.Kind() != "qualified_name" {
			s.unknown(indexes)
			return nil
		}
		s.forward(indexes, ParamForward{Kind: ForwardStatic, Class: v.resolveFQCN(scopeName), Method: method, Pos: pos})
	}
	return nil
}
