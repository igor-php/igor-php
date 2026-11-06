package analyzer

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// restoreContext says where a property write sits relative to the try statements around it.
type restoreContext struct {
	inFinally bool
	inCatch   bool
	// catchCoversAll is set when the innermost catch also catches \Error (\Throwable, or both
	// \Exception and \Error).
	catchCoversAll bool
	catchTypes     string
}

// localSave is an assignment to a local variable: prop is the $this property it copied, or ""
// when it assigned something else.
type localSave struct {
	node *sitter.Node
	prop string
}

// savedProperty returns the property a local variable holds when restore runs: the one its latest
// save before restore copied. A later assignment of something else overwrites it, unless it sits in
// a branch that leaves (a catch that rethrows) and so never reaches restore.
func savedProperty(saves map[string][]localSave, variable string, restore *sitter.Node) string {
	prop := ""
	for _, s := range saves[variable] {
		if s.node.StartByte() >= restore.StartByte() {
			break
		}
		if s.prop != "" || sameFlow(s.node, restore) || !leaves(enclosingBlock(s.node)) {
			prop = s.prop
		}
	}
	return prop
}

type propertyWrite struct {
	node  *sitter.Node
	prop  string
	value string // trimmed source of the assigned value
	ctx   restoreContext
}

// checkUnsafeRestores reports the properties a method sets and restores later, when the restoration
// does not run if an exception escapes: it sits after the work instead of in a finally block, or in a
// catch that lets \Error through. The service then keeps the temporary value for the next requests.
func (v *PHPVisitor) checkUnsafeRestores(method *sitter.Node) {
	if v.curClass == "" || v.curMethod == "" || strings.EqualFold(v.curMethod, "__construct") || v.isReset || v.isReadonlyClass {
		return
	}
	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}
	// A transient (shared: false) service does not outlive the request that throws
	if v.isTransientClass(fullName) {
		return
	}
	body := method.ChildByFieldName("body")
	if body == nil {
		return
	}

	var writes []propertyWrite
	saves := make(map[string][]localSave) // local variable => its assignments, in source order
	var calls []uint                      // start bytes of the expressions that can throw
	v.collectRestoreEvents(body, restoreContext{}, &writes, saves, &calls)

	reported := make(map[string]bool)
	for i, w := range writes {
		if reported[w.prop] || v.readonlyProps[w.prop] || v.workerSafeProps[w.prop] {
			continue
		}
		set := v.restoredWrite(writes[:i], w, saves)
		if set == nil || !throwsBetween(calls, set.node.EndByte(), w.node.StartByte()) {
			continue
		}
		if v.restoredSafely(writes, set, saves) || swallowedBeforeRestore(calls, set.node, w.node, v.catchesEverything) {
			continue
		}
		reported[w.prop] = true
		if w.ctx.inCatch {
			v.addFinding(w.node,
				fmt.Sprintf("Property '%s' is restored in catch (%s): an \\Error leaves it set for the next requests.", w.prop, w.ctx.catchTypes),
				"Restore it in a finally block, or catch \\Throwable.", "ERROR")
			continue
		}
		v.addFinding(w.node,
			fmt.Sprintf("Property '%s' is restored without finally: an exception thrown after line %d leaves it set for the next requests.", w.prop, set.node.StartPosition().Row+1),
			"Restore it in a finally block so that it is restored whatever happens.", "ERROR")
	}
}

// restoredSafely reports whether one of the method's restorations of the write set runs on every
// exception: in a finally block, or in a catch that also catches \Error. A safe restoration of
// another write of the same property does not protect this one.
func (v *PHPVisitor) restoredSafely(writes []propertyWrite, set *propertyWrite, saves map[string][]localSave) bool {
	for i, w := range writes {
		if w.prop != set.prop || (!w.ctx.inFinally && !(w.ctx.inCatch && w.ctx.catchCoversAll)) {
			continue
		}
		if v.restoredWrite(writes[:i], w, saves) == set {
			return true
		}
	}
	return false
}

// restoredWrite returns the earlier write that w restores, or nil when w is no restoration:
// w assigns the value saved from the property before it was set, or switches a boolean back,
// or resets to null a value set earlier.
func (v *PHPVisitor) restoredWrite(before []propertyWrite, w propertyWrite, saves map[string][]localSave) *propertyWrite {
	lower := strings.ToLower(w.value)
	for i := len(before) - 1; i >= 0; i-- {
		set := &before[i]
		// Skip the writes of the same value: another restoration (in the try body before its catch)
		if set.prop != w.prop || set.ctx.inFinally || set.ctx.inCatch || strings.EqualFold(set.value, w.value) {
			continue
		}
		// The restoration must run after the write on the normal path: in the same block, or in the
		// try, catch or finally clauses of a try statement of that block, not in a condition or a loop
		if !sameFlow(set.node, w.node) {
			return nil
		}
		setLower := strings.ToLower(set.value)
		switch {
		case savedProperty(saves, w.value, w.node) == w.prop:
			return set
		case (setLower == "true" && lower == "false") || (setLower == "false" && lower == "true"):
			return set
		case lower == "null" && setLower != "null":
			return set
		}
		return nil
	}
	return nil
}

// collectRestoreEvents walks a method body in source order. It records the writes to $this
// properties with their try context, the local variables saving a property, and the expressions
// that can throw. Closures are skipped: they run later, in another scope.
func (v *PHPVisitor) collectRestoreEvents(n *sitter.Node, ctx restoreContext, writes *[]propertyWrite, saves map[string][]localSave, calls *[]uint) {
	switch n.Kind() {
	case "anonymous_function", "arrow_function", "anonymous_class", "class_declaration", "function_definition":
		return
	case "catch_clause":
		ctx.inCatch = true
		ctx.catchTypes = ""
		if types := n.ChildByFieldName("type"); types != nil {
			ctx.catchTypes = v.getContent(types)
		}
		ctx.catchCoversAll = v.catchCoversAll(ctx.catchTypes)
	case "finally_clause":
		ctx.inFinally = true
	case "function_call_expression", "member_call_expression", "nullsafe_member_call_expression",
		"scoped_call_expression", "object_creation_expression", "throw_expression", "include_expression", "require_expression":
		*calls = append(*calls, n.StartByte())
	case "assignment_expression":
		left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		if left != nil && right != nil {
			if prop, ok := v.isPropertyFetchOnThis(left); ok && v.getContent(left.ChildByFieldName("object")) == "$this" {
				*writes = append(*writes, propertyWrite{node: n, prop: prop, value: strings.TrimSpace(v.getContent(right)), ctx: ctx})
			} else if left.Kind() == "variable_name" {
				// Any other assignment overwrites the saved value
				prop := ""
				if p, ok := v.isPropertyFetchOnThis(right); ok && v.getContent(right.ChildByFieldName("object")) == "$this" {
					prop = p
				}
				name := v.getContent(left)
				saves[name] = append(saves[name], localSave{node: n, prop: prop})
			}
		}
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		v.collectRestoreEvents(n.Child(i), ctx, writes, saves, calls)
	}
}

// catchCoversAll reports whether a catch type list also catches \Error. The types are resolved
// through the imports and the namespace: an unqualified Exception in a namespace without import
// names a class of that namespace, not \Exception.
func (v *PHPVisitor) catchCoversAll(types string) bool {
	hasException, hasError := false, false
	for _, t := range strings.Split(types, "|") {
		name := strings.TrimSpace(t)
		if name == "" {
			continue
		}
		switch strings.ToLower(v.resolveFQCN(name)) {
		case "throwable":
			return true
		case "exception":
			hasException = true
		case "error":
			hasError = true
		}
	}
	return hasException && hasError
}

// throwsBetween reports whether an expression that can throw starts between two offsets.
func throwsBetween(calls []uint, from, to uint) bool {
	for _, c := range calls {
		if c >= from && c < to {
			return true
		}
	}
	return false
}

// sameFlow reports whether restore runs whenever set ran: walking up from restore to the block of
// set, or to a block that holds it through try statements, only crosses try statements and their
// clauses.
func sameFlow(set, restore *sitter.Node) bool {
	block := set.Parent()
	for block != nil && block.Kind() != "compound_statement" {
		block = block.Parent()
	}
	// A write in a try body is restored by the catch and finally clauses of the same try, or after
	// the try statement, in the block that holds it
	var scopes []*sitter.Node
	for block != nil {
		scopes = append(scopes, block)
		try := block.Parent()
		if try == nil || try.Kind() != "try_statement" {
			break
		}
		scopes = append(scopes, try)
		block = try.Parent()
		if block != nil && block.Kind() != "compound_statement" {
			break
		}
	}
	for n := restore.Parent(); n != nil; n = n.Parent() {
		for _, scope := range scopes {
			if sameNode(n, scope) && n.Kind() == scope.Kind() {
				return true
			}
		}
		switch n.Kind() {
		case "expression_statement", "compound_statement", "try_statement", "catch_clause", "finally_clause":
		default:
			return false
		}
	}
	return false
}

// swallowedBeforeRestore reports whether every exception between set and restore is swallowed: set
// sits in the body of a try statement that restore follows, a catch of that statement catches
// everything without leaving (no throw or return), and nothing outside the try body can throw
// before restore.
func swallowedBeforeRestore(calls []uint, set, restore *sitter.Node, catchesEverything func(*sitter.Node) bool) bool {
	var try *sitter.Node
	for p := set.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "try_statement" {
			try = p
			break
		}
	}
	if try == nil || try.EndByte() > restore.StartByte() {
		return false
	}
	body := try.ChildByFieldName("body")
	if body == nil || set.StartByte() < body.StartByte() || set.EndByte() > body.EndByte() {
		return false
	}
	// One catch must catch everything, and no catch nor the finally may skip the restore
	swallows := false
	for i := uint(0); i < try.NamedChildCount(); i++ {
		clause := try.NamedChild(i)
		switch clause.Kind() {
		case "catch_clause", "finally_clause":
			if leaves(clause) {
				return false
			}
			swallows = swallows || (clause.Kind() == "catch_clause" && catchesEverything(clause))
		}
	}
	if !swallows {
		return false
	}
	for _, c := range calls {
		inBody := c >= body.StartByte() && c < body.EndByte()
		if c >= set.EndByte() && c < restore.StartByte() && !inBody {
			return false
		}
	}
	return true
}

// catchesEverything reports whether a catch clause also catches \Error.
func (v *PHPVisitor) catchesEverything(clause *sitter.Node) bool {
	types := clause.ChildByFieldName("type")
	return types != nil && v.catchCoversAll(v.getContent(types))
}

// enclosingBlock returns the innermost block that holds n, or n itself when there is none.
func enclosingBlock(n *sitter.Node) *sitter.Node {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "compound_statement" {
			return p
		}
	}
	return n
}

// leaves reports whether a clause may skip the code after its try statement: a throw, a return, an
// exit, a break or a continue in it. Closures run later and are not followed.
func leaves(n *sitter.Node) bool {
	switch n.Kind() {
	case "throw_expression", "return_statement", "exit_statement", "break_statement", "continue_statement":
		return true
	case "anonymous_function", "arrow_function", "anonymous_class":
		return false
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		if leaves(n.Child(i)) {
			return true
		}
	}
	return false
}
