package analyzer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/igor-php/igor-php/pkg/symbol"
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

// reachingSaves returns the assignments of a local variable that may still hold their value when
// restore runs. An assignment that runs whenever restore does replaces them all, and so do the
// branches of an if statement that assigns the variable on every branch that goes on; one that runs
// on some paths only adds to them. Assignments of something else than a property have no prop.
func (v *PHPVisitor) reachingSaves(saves map[string][]localSave, variable string, restore *sitter.Node) []localSave {
	var reach []localSave
	var replacedBy *sitter.Node // the if statement whose branches replaced the earlier saves
	for _, s := range saves[variable] {
		if s.node.StartByte() >= restore.StartByte() {
			break
		}
		switch {
		case sameFlow(s.node, restore):
			reach = []localSave{s}
		case v.overwrittenOnEveryBranch(s.node, variable, restore):
			if stmt := enclosingIf(s.node); replacedBy == nil || !sameNode(stmt, replacedBy) {
				reach, replacedBy = nil, stmt
			}
			reach = append(reach, s)
		case leavesAfter(s.node, restore):
			// The block of the assignment returns or throws: it never reaches restore
		default:
			reach = append(reach, s)
		}
	}
	return reach
}

// leavesAfter reports whether the block holding a statement always leaves the function without
// reaching restore. A throw inside a try statement may be caught and go on to restore, and a
// return inside a try statement that holds restore still runs its finally: the assignment is kept.
func leavesAfter(n, restore *sitter.Node) bool {
	stmt := n.Parent()
	if stmt == nil || stmt.Kind() != "expression_statement" {
		return false
	}
	block := stmt.Parent()
	if block == nil || block.Kind() != "compound_statement" || !alwaysLeaves(block) {
		return false
	}
	throws := !returnsDirectly(block)
	for p := block.Parent(); p != nil; p = p.Parent() {
		switch p.Kind() {
		case "try_statement":
			if restore.StartByte() >= p.StartByte() && restore.EndByte() <= p.EndByte() {
				return false
			}
			// A catch of the statement may catch the throw and go on to restore
			if body := p.ChildByFieldName("body"); throws && hasCatch(p) && body != nil && block.StartByte() >= body.StartByte() && block.EndByte() <= body.EndByte() {
				return false
			}
		case "method_declaration", "function_definition", "anonymous_function", "arrow_function":
			return true
		}
	}
	return true
}

// hasCatch reports whether a try statement has a catch clause.
func hasCatch(try *sitter.Node) bool {
	for i := uint(0); i < try.NamedChildCount(); i++ {
		if try.NamedChild(i).Kind() == "catch_clause" {
			return true
		}
	}
	return false
}

// returnsDirectly reports whether one of a block's own statements returns or exits.
func returnsDirectly(block *sitter.Node) bool {
	for i := uint(0); i < block.NamedChildCount(); i++ {
		switch block.NamedChild(i).Kind() {
		case "return_statement", "exit_statement":
			return true
		}
	}
	return false
}

// enclosingIf returns the nearest if statement that holds n, or nil.
func enclosingIf(n *sitter.Node) *sitter.Node {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "if_statement" {
			return p
		}
	}
	return nil
}

// overwrittenOnEveryBranch reports whether the if statement that holds an assignment assigns the
// variable on every branch that goes on: it has an else, and each branch assigns it or leaves.
func (v *PHPVisitor) overwrittenOnEveryBranch(assignment *sitter.Node, variable string, restore *sitter.Node) bool {
	stmt := enclosingIf(assignment)
	if stmt == nil || !sameFlow(stmt, restore) {
		return false
	}
	branches := []*sitter.Node{stmt.ChildByFieldName("body")}
	hasElse := false
	for i := uint(0); i < stmt.NamedChildCount(); i++ {
		switch clause := stmt.NamedChild(i); clause.Kind() {
		case "else_clause", "else_if_clause":
			hasElse = hasElse || clause.Kind() == "else_clause"
			branches = append(branches, clause.ChildByFieldName("body"))
		}
	}
	if !hasElse {
		return false
	}
	for _, b := range branches {
		if b == nil || (!v.alwaysAssigns(b, variable) && !alwaysLeaves(b)) {
			return false
		}
	}
	return true
}

// alwaysAssigns reports whether a block assigns a local variable in one of its own statements, so
// on every path through it.
func (v *PHPVisitor) alwaysAssigns(block *sitter.Node, variable string) bool {
	for i := uint(0); i < block.NamedChildCount(); i++ {
		stmt := block.NamedChild(i)
		if stmt.Kind() != "expression_statement" || stmt.NamedChildCount() == 0 {
			continue
		}
		expr := stmt.NamedChild(0)
		if expr.Kind() != "assignment_expression" {
			continue
		}
		if left := expr.ChildByFieldName("left"); left != nil && left.Kind() == "variable_name" && v.getContent(left) == variable {
			return true
		}
	}
	return false
}

// alwaysLeaves reports whether a block ends every path through it: one of its own statements
// returns, throws or exits.
func alwaysLeaves(block *sitter.Node) bool {
	for i := uint(0); i < block.NamedChildCount(); i++ {
		stmt := block.NamedChild(i)
		switch stmt.Kind() {
		case "return_statement", "exit_statement":
			return true
		case "expression_statement":
			if stmt.NamedChildCount() > 0 && stmt.NamedChild(0).Kind() == "throw_expression" {
				return true
			}
		}
	}
	return false
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
		if v.restoredSafely(writes, set, saves, calls) || v.swallowedBeforeRestore(calls, set.node, w.node) {
			continue
		}
		reported[w.prop] = true
		if w.ctx.inCatch {
			v.addFinding(w.node, symbol.RuleUnsafeStateRestore,
				fmt.Sprintf("Property '%s' is restored in catch (%s): an \\Error leaves it set for the next requests.", w.prop, w.ctx.catchTypes),
				"Restore it in a finally block, or catch \\Throwable.", "ERROR")
			continue
		}
		v.addFinding(w.node, symbol.RuleUnsafeStateRestore,
			fmt.Sprintf("Property '%s' is restored without finally: an exception thrown after line %d leaves it set for the next requests.", w.prop, set.node.StartPosition().Row+1),
			"Restore it in a finally block so that it is restored whatever happens.", "ERROR")
	}
}

// restoredSafely reports whether one of the method's restorations of the write set runs on every
// exception: in a finally block, or in a catch that also catches \Error, with nothing before it in
// the clause that can throw past it. A safe restoration of another write of the same property does
// not protect this one.
func (v *PHPVisitor) restoredSafely(writes []propertyWrite, set *propertyWrite, saves map[string][]localSave, calls []uint) bool {
	for i, w := range writes {
		if w.prop != set.prop || (!w.ctx.inFinally && !(w.ctx.inCatch && w.ctx.catchCoversAll)) {
			continue
		}
		// A restoration of a saved value must restore it on every path. It then protects every write
		// made after the save that it follows, even when another restoration sits in between
		if len(saves[w.value]) > 0 {
			if v.savedOnEveryPath(saves, w, set.node) && w.node.StartByte() > set.node.EndByte() &&
				sameFlow(set.node, w.node) && v.reachedInClause(calls, w.node) {
				return true
			}
			continue
		}
		if v.restoredWrite(writes[:i], w, saves) == set && v.reachedInClause(calls, w.node) {
			return true
		}
	}
	return false
}

// reachedInClause reports whether a restoration in a catch or finally clause always runs once the
// clause starts: whatever can throw before it in the clause is swallowed there.
func (v *PHPVisitor) reachedInClause(calls []uint, restore *sitter.Node) bool {
	for p := restore.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "catch_clause" || p.Kind() == "finally_clause" {
			return v.swallowedBetween(calls, p.StartByte(), restore)
		}
	}
	return true
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
		// The value must be saved before the write: a save after it holds the temporary value
		switch {
		case v.savedBefore(saves, w, set.node):
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

// savedBefore reports whether the variable w assigns may hold, on one path, the value of its property
// saved before the write set.
func (v *PHPVisitor) savedBefore(saves map[string][]localSave, w propertyWrite, set *sitter.Node) bool {
	for _, s := range v.reachingSaves(saves, w.value, w.node) {
		if s.prop == w.prop && s.node.StartByte() < set.StartByte() {
			return true
		}
	}
	return false
}

// savedOnEveryPath reports whether the variable w assigns holds, on every path, the value of its
// property saved before the write set: a cleanup that only restores on some paths protects nothing.
func (v *PHPVisitor) savedOnEveryPath(saves map[string][]localSave, w propertyWrite, set *sitter.Node) bool {
	reach := v.reachingSaves(saves, w.value, w.node)
	for _, s := range reach {
		if s.prop != w.prop || s.node.StartByte() >= set.StartByte() {
			return false
		}
	}
	return len(reach) > 0
}

// runtimeCleanupFunctions restore the process state without throwing.
var runtimeCleanupFunctions = map[string]bool{
	"restore_error_handler": true, "restore_exception_handler": true, "gc_enable": true, "gc_disable": true,
	"error_reporting": true, "ini_restore": true,
}

// isRuntimeCleanup reports a call of a PHP function that restores the process state, unless the
// name is shadowed by an imported or namespace function.
func (v *PHPVisitor) isRuntimeCleanup(call *sitter.Node) bool {
	nameNode := call.ChildByFieldName("function")
	if nameNode == nil {
		return false
	}
	raw := v.getContent(nameNode)
	name := strings.ToLower(strings.TrimPrefix(raw, "\\"))
	if !runtimeCleanupFunctions[name] {
		return false
	}
	if !strings.HasPrefix(raw, "\\") {
		if shadowed := v.getShadowedFunctions(call.StartByte()); shadowed != nil && shadowed[name] {
			return false
		}
	}
	return true
}

// stackFunctions push to or pop from an array without throwing.
var stackFunctions = map[string]bool{"array_pop": true, "array_push": true, "array_shift": true, "array_unshift": true}

// isStackCleanup reports a call of a stack function on a property of the service, the array the
// method pushed to: array_pop($this->stack). On another argument, a null one throws a TypeError.
func (v *PHPVisitor) isStackCleanup(call *sitter.Node) bool {
	name, args := call.ChildByFieldName("function"), call.ChildByFieldName("arguments")
	if name == nil || args == nil || !stackFunctions[strings.ToLower(strings.TrimPrefix(v.getContent(name), "\\"))] {
		return false
	}
	// The array is the first argument, positional or named (array_pop(array: $this->stack))
	first := args.NamedChild(0)
	if first == nil || first.Kind() != "argument" || first.NamedChildCount() == 0 {
		return false
	}
	if name := first.ChildByFieldName("name"); name != nil && v.getContent(name) != "array" {
		return false
	}
	value := first.NamedChild(first.NamedChildCount() - 1)
	_, ok := v.isPropertyFetchOnThis(value)
	return ok && v.getContent(value.ChildByFieldName("object")) == "$this"
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
	case "function_call_expression":
		// A cleanup of the service's own stack (array_pop($this->stack)) or of the process state
		// (restore_error_handler()) does not throw
		if !v.isStackCleanup(n) && !v.isRuntimeCleanup(n) {
			*calls = append(*calls, n.StartByte())
		}
	case "member_call_expression", "nullsafe_member_call_expression",
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

// swallowedBeforeRestore reports whether every exception thrown between set and restore is
// swallowed before restore: each expression that can throw sits in the body of a try statement that
// restore follows, with a catch that catches everything and no clause that leaves.
func (v *PHPVisitor) swallowedBeforeRestore(calls []uint, set, restore *sitter.Node) bool {
	return v.swallowedBetween(calls, set.EndByte(), restore)
}

// swallowedBetween reports whether every expression that can throw from the byte from to restore
// is swallowed by a try statement that ends before restore.
func (v *PHPVisitor) swallowedBetween(calls []uint, from uint, restore *sitter.Node) bool {
	root := restore
	for root.Parent() != nil {
		root = root.Parent()
	}
	for _, c := range calls {
		if c >= from && c < restore.StartByte() && !v.swallowedAt(root, c, restore) {
			return false
		}
	}
	return true
}

// swallowedAt reports whether the expression starting at byte c sits in the body of a try statement
// that ends before restore, catches everything and has no clause that leaves.
func (v *PHPVisitor) swallowedAt(root *sitter.Node, c uint, restore *sitter.Node) bool {
	for p := root.DescendantForByteRange(c, c); p != nil; p = p.Parent() {
		if p.Kind() != "try_statement" || p.EndByte() > restore.StartByte() {
			continue
		}
		body := p.ChildByFieldName("body")
		if body != nil && c >= body.StartByte() && c < body.EndByte() && v.swallows(p) {
			return true
		}
	}
	return false
}

// swallows reports whether a try statement swallows every exception of its body: a catch catches
// everything, and no catch nor the finally may skip the code after the statement.
func (v *PHPVisitor) swallows(try *sitter.Node) bool {
	swallows := false
	for i := uint(0); i < try.NamedChildCount(); i++ {
		clause := try.NamedChild(i)
		switch clause.Kind() {
		case "catch_clause", "finally_clause":
			if v.leaves(clause) {
				return false
			}
			swallows = swallows || (clause.Kind() == "catch_clause" && v.catchesEverything(clause))
		}
	}
	return swallows
}

// catchesEverything reports whether a catch clause also catches \Error.
func (v *PHPVisitor) catchesEverything(clause *sitter.Node) bool {
	types := clause.ChildByFieldName("type")
	return types != nil && v.catchCoversAll(v.getContent(types))
}

// leaves reports whether a clause may skip the code after its try statement: a throw, a return, an
// exit, a break or a continue in it. Closures run later and are not followed.
func (v *PHPVisitor) leaves(n *sitter.Node) bool {
	return v.leavesFrom(n, 0)
}

// leavesFrom counts the loops and switches of the clause around n: a break or a continue only stays
// in the clause when its level (break 2) does not exceed them.
func (v *PHPVisitor) leavesFrom(n *sitter.Node, depth int) bool {
	switch n.Kind() {
	case "throw_expression", "return_statement", "exit_statement":
		return true
	case "break_statement", "continue_statement":
		return v.jumpLevel(n) > depth
	case "anonymous_function", "arrow_function", "anonymous_class":
		return false
	case "for_statement", "foreach_statement", "while_statement", "do_statement", "switch_statement":
		depth++
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		if v.leavesFrom(n.Child(i), depth) {
			return true
		}
	}
	return false
}

// jumpLevel returns how many loops a break or a continue ends: its argument, 1 by default.
func (v *PHPVisitor) jumpLevel(n *sitter.Node) int {
	if n.NamedChildCount() == 0 {
		return 1
	}
	level, err := strconv.Atoi(strings.TrimSpace(v.getContent(n.NamedChild(0))))
	if err != nil || level < 1 {
		return 1
	}
	return level
}
