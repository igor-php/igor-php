package analyzer

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// doctrineCollectEvents are the Doctrine events that run before the commit: what a listener
// collects there is only safe to clear in postFlush if the commit succeeds.
var doctrineCollectEvents = []string{"onFlush", "preFlush", "prePersist", "preUpdate", "preRemove"}

type flushWrite struct {
	node  *sitter.Node
	clear bool // assigns an empty value or unsets the property
}

// checkDoctrineFlushState reports a Doctrine listener that collects state in onFlush (or another
// pre-commit event) and only clears it in postFlush. Doctrine skips postFlush when the commit fails
// (constraint violation, deadlock, lost connection), so the shared listener keeps the objects of the
// failed request and processes them during the next flush of the worker.
func (v *PHPVisitor) checkDoctrineFlushState(n *sitter.Node) {
	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}
	if v.isReset || v.isReadonlyClass || v.isTransientClass(fullName) {
		return
	}
	methods := v.declaredMethods(n)
	clearedInPostFlush := v.onlyClearedInPostFlush(methods)
	if len(clearedInPostFlush) == 0 {
		return
	}

	reported := make(map[string]bool)
	for _, event := range doctrineCollectEvents {
		method := methods[strings.ToLower(event)]
		if method == nil || v.hasAttribute(method, "WorkerSafe") {
			continue
		}
		for prop, writes := range v.collectFlushWrites(method) {
			if !clearedInPostFlush[prop] || reported[prop] || v.workerSafeProps[prop] || clearedByEvent(writes, method) {
				continue
			}
			// An ignored write does not hide the other events collecting the property
			first := v.firstCollectingWrite(writes)
			if first == nil {
				continue
			}
			reported[prop] = true
			saved := v.curMethod
			v.curMethod = event
			v.addFinding(first.node,
				fmt.Sprintf("Property '%s' collected in %s() is only cleared in postFlush(), which Doctrine skips when the commit fails: the next flush of the worker processes it.", prop, event),
				fmt.Sprintf("Implement ResetInterface (tagged kernel.reset) to clear it between requests, or clear it at the start of %s().", event),
				"ERROR")
			v.curMethod = saved
		}
	}
}

// declaredMethods maps the lowercase name of each method a class declares to its declaration.
func (v *PHPVisitor) declaredMethods(class *sitter.Node) map[string]*sitter.Node {
	methods := make(map[string]*sitter.Node)
	body := v.getClassBody(class)
	if body == nil {
		return methods
	}
	for i := uint(0); i < body.NamedChildCount(); i++ {
		member := body.NamedChild(i)
		if member.Kind() != "method_declaration" {
			continue
		}
		if nameNode := member.ChildByFieldName("name"); nameNode != nil {
			methods[strings.ToLower(v.getContent(nameNode))] = member
		}
	}
	return methods
}

// onlyClearedInPostFlush returns the properties postFlush() clears and that no other method clears
// (onClear, a reset hook...), apart from the pre-commit events themselves.
func (v *PHPVisitor) onlyClearedInPostFlush(methods map[string]*sitter.Node) map[string]bool {
	cleared := make(map[string]bool)
	postFlush := methods["postflush"]
	if postFlush == nil {
		return cleared
	}
	for prop := range clearedProps(v.collectFlushWrites(postFlush)) {
		cleared[prop] = true
	}
	for name, method := range methods {
		if name == "postflush" || name == "__construct" || isCollectEvent(name) {
			continue
		}
		for prop := range clearedProps(v.collectFlushWrites(method)) {
			delete(cleared, prop)
		}
	}
	return cleared
}

func clearedProps(writes map[string][]flushWrite) map[string]bool {
	cleared := make(map[string]bool)
	for prop, list := range writes {
		for _, w := range list {
			if w.clear {
				cleared[prop] = true
			}
		}
	}
	return cleared
}

func isCollectEvent(lowerName string) bool {
	for _, e := range doctrineCollectEvents {
		if strings.ToLower(e) == lowerName {
			return true
		}
	}
	return false
}

// clearedByEvent reports whether the event itself leaves nothing behind for a failed commit: it
// starts by clearing the property on every path, or every collecting write is followed by a clear
// on every path of a finally block of its try statement.
func clearedByEvent(writes []flushWrite, method *sitter.Node) bool {
	if writes[0].clear && unconditional(writes[0].node, method.ChildByFieldName("body")) {
		return true
	}
	// A clear in finally must run whenever the finally block does (no condition inside it either).
	// Successive try/finally blocks may each cover their own writes.
	var clears []*sitter.Node
	for _, w := range writes {
		if w.clear {
			if finally := enclosingFinally(w.node, method); finally != nil && unconditional(w.node, finally) {
				clears = append(clears, w.node)
			}
		}
	}
	if len(clears) == 0 {
		return false
	}
	for _, w := range writes {
		if !w.clear && !coveredByClear(w.node, clears, method) {
			return false
		}
	}
	return true
}

// coveredByClear reports whether one of the finally clears sits in the finally of a try statement
// holding the write, after it.
func coveredByClear(write *sitter.Node, clears []*sitter.Node, method *sitter.Node) bool {
	// A callback may run after the clear: its writes are never covered
	for p := write.Parent(); p != nil && !sameNode(p, method); p = p.Parent() {
		if p.Kind() == "anonymous_function" || p.Kind() == "arrow_function" {
			return false
		}
	}
	start := write.StartByte()
	for _, clear := range clears {
		try := enclosingFinally(clear, method).Parent()
		if try != nil && start >= try.StartByte() && start < clear.StartByte() {
			return true
		}
	}
	return false
}

// unconditional reports whether a statement runs whenever the enclosing scope does: only plain
// blocks separate it from the scope, no condition, loop or catch.
func unconditional(n, scope *sitter.Node) bool {
	if scope == nil {
		return false
	}
	for p := n.Parent(); p != nil; p = p.Parent() {
		if sameNode(p, scope) {
			return true
		}
		switch p.Kind() {
		case "expression_statement", "compound_statement":
		default:
			return false
		}
	}
	return false
}

// enclosingFinally returns the innermost finally clause of the method that holds n, or nil.
func enclosingFinally(n, method *sitter.Node) *sitter.Node {
	for p := n.Parent(); p != nil && !sameNode(p, method); p = p.Parent() {
		if p.Kind() == "finally_clause" {
			return p
		}
	}
	return nil
}

// firstCollectingWrite returns the first collecting write not marked @igor-ignore, or nil.
func (v *PHPVisitor) firstCollectingWrite(writes []flushWrite) *flushWrite {
	for i := range writes {
		if !writes[i].clear && !v.isIgnoredLine(writes[i].node) {
			return &writes[i]
		}
	}
	return nil
}

// collectFlushWrites returns, in source order, the writes of each $this property in a method.
// Closures are included: Doctrine listeners often collect in a loop callback. Nested classes and
// functions are not: $this is another object there, or none.
func (v *PHPVisitor) collectFlushWrites(method *sitter.Node) map[string][]flushWrite {
	writes := make(map[string][]flushWrite)
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		switch n.Kind() {
		case "anonymous_class", "class_declaration", "function_definition":
			return
		case "assignment_expression", "augmented_assignment_expression":
			left := n.ChildByFieldName("left")
			if prop, direct, ok := v.flushWriteTarget(left); ok {
				clear := direct && n.Kind() == "assignment_expression" && v.isClearingValue(v.getContent(n.ChildByFieldName("right")))
				writes[prop] = append(writes[prop], flushWrite{node: n, clear: clear})
			}
		case "update_expression":
			for i := uint(0); i < n.NamedChildCount(); i++ {
				if prop, _, ok := v.flushWriteTarget(n.NamedChild(i)); ok {
					writes[prop] = append(writes[prop], flushWrite{node: n})
				}
			}
		case "unset_statement":
			for i := uint(0); i < n.NamedChildCount(); i++ {
				if prop, direct, ok := v.flushWriteTarget(n.NamedChild(i)); ok {
					writes[prop] = append(writes[prop], flushWrite{node: n, clear: direct})
				}
			}
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(method)
	return writes
}

// flushWriteTarget returns the property written through $this->prop or $this->prop[...],
// and whether the write targets the property itself (direct) rather than one of its elements.
func (v *PHPVisitor) flushWriteTarget(n *sitter.Node) (string, bool, bool) {
	direct := true
	for n != nil && n.Kind() == "subscript_expression" && n.ChildCount() > 0 {
		n = n.Child(0)
		direct = false
	}
	if n == nil || n.Kind() != "member_access_expression" {
		return "", false, false
	}
	if obj := n.ChildByFieldName("object"); obj == nil || v.getContent(obj) != "$this" {
		return "", false, false
	}
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return "", false, false
	}
	return v.getContent(nameNode), direct, true
}

// isClearingValue reports whether an assigned value empties a collection or a reference. The class
// of an empty collection is resolved through the imports (use ArrayCollection as Collection).
func (v *PHPVisitor) isClearingValue(value string) bool {
	compact := strings.Join(strings.Fields(value), "")
	switch strings.ToLower(compact) {
	case "[]", "array()", "null", "0", "false", "''", `""`, "newarraycollection()", `new\doctrine\common\collections\arraycollection()`, "new\\splobjectstorage()", "newsplobjectstorage()":
		return true
	}
	class, ok := strings.CutSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "new")), "()")
	if !ok || !strings.HasPrefix(strings.ToLower(compact), "new") {
		return false
	}
	switch strings.ToLower(v.resolveFQCN(strings.TrimSpace(class))) {
	case `doctrine\common\collections\arraycollection`, "splobjectstorage":
		return true
	}
	return false
}
