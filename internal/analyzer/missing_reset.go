package analyzer

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// checkMissingResetTag reports a shared service whose class shows the intent to be reset between
// requests, but that the container never resets: it implements ResetInterface, or has a public
// reset() or clear() method rewriting properties that its other methods mutate, and none of its
// definitions (nor the decorator that took over their tags) is tagged kernel.reset.
func (v *PHPVisitor) checkMissingResetTag(n *sitter.Node, fullName string) {
	if v.isWorkerSafeClass || v.engine == nil || !v.engine.MissingResetTag(fullName) {
		return
	}
	body := v.getClassBody(n)
	if body == nil {
		return
	}

	method := v.unwiredResetMethod(n, body)
	if method == "" {
		return
	}

	target := n.ChildByFieldName("name")
	if target == nil {
		target = n
	}
	v.addFinding(target,
		fmt.Sprintf("%s has %s() but its service is never reset between requests (missing kernel.reset tag).", v.curClass, method),
		fmt.Sprintf("Tag the service so services_resetter calls it after each request: ->tag('kernel.reset', ['method' => '%s', 'on_invalid' => 'ignore']). "+
			"on_invalid: ignore keeps the container valid when a decorator replaces the service, since the resetter calls the outermost one.", method),
		"ERROR")
}

// unwiredResetMethod returns the reset() or clear() method that shows the intent to reset the class,
// or "" when it has none.
func (v *PHPVisitor) unwiredResetMethod(class, body *sitter.Node) string {
	cleared := make(map[string]map[string]bool) // reset method (lowercase) => properties it rewrites
	written := make(map[string]bool)            // properties mutated by the other methods
	resetMethods := make(map[string]string)     // lowercase => declared name
	for i := uint(0); i < body.NamedChildCount(); i++ {
		member := body.NamedChild(i)
		nameNode := member.ChildByFieldName("name")
		if member.Kind() != "method_declaration" || nameNode == nil {
			continue
		}
		name := v.getContent(nameNode)
		lower := strings.ToLower(name)
		switch {
		case lower == "reset" || lower == "clear":
			// services_resetter calls the method without arguments
			if v.isPublicMethod(member) && callableWithoutArguments(member) {
				resetMethods[lower] = name
				cleared[lower] = make(map[string]bool)
				v.collectThisWrites(member, cleared[lower])
			}
		case lower != "__construct" && !v.hasAttribute(member, "WorkerSafe"):
			mutated := make(map[string]bool)
			v.collectThisWrites(member, mutated)
			// The method already frees what it cleans up in a finally block, as the mutation check assumes
			cleaned := v.finallyCleanedIn(member)
			for prop := range mutated {
				if !cleaned[prop] {
					written[prop] = true
				}
			}
		}
	}
	// The properties excluded from the mutation checks do not need a reset either
	for prop := range written {
		if v.workerSafeProps[prop] || v.readonlyProps[prop] {
			delete(written, prop)
		}
	}

	if v.implementsResetInterface(class) && resetMethods["reset"] != "" {
		return resetMethods["reset"]
	}
	for _, candidate := range []string{"reset", "clear"} {
		if resetMethods[candidate] != "" && intersects(cleared[candidate], written) {
			return resetMethods[candidate]
		}
	}
	return ""
}

// collectThisWrites records the $this properties a method assigns, appends to or increments.
// Nested classes and functions are skipped: $this is another object there, or none. So are the
// lines marked @igor-ignore.
func (v *PHPVisitor) collectThisWrites(n *sitter.Node, props map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "anonymous_class", "class_declaration", "function_definition":
		return
	case "assignment_expression", "augmented_assignment_expression":
		left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
		// A lazy initializer builds the same object for every request
		if left != nil && right != nil && v.isLazyInit(n, left, right) {
			break
		}
		if prop, ok := v.thisWriteTarget(left); ok && !v.isIgnoredLine(n) {
			props[prop] = true
		}
	case "update_expression":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			if prop, ok := v.thisWriteTarget(n.NamedChild(i)); ok && !v.isIgnoredLine(n) {
				props[prop] = true
			}
		}
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		v.collectThisWrites(n.Child(i), props)
	}
}

// finallyCleanedIn returns the properties a method cleans up in its finally blocks, with the checks
// the mutation check runs on them. Nested classes and functions are skipped, as in collectThisWrites:
// their $this is another object.
func (v *PHPVisitor) finallyCleanedIn(method *sitter.Node) map[string]bool {
	cleaned := make(map[string]bool)
	var walk func(n *sitter.Node, inFinally bool)
	walk = func(n *sitter.Node, inFinally bool) {
		switch n.Kind() {
		case "anonymous_class", "class_declaration", "function_definition":
			return
		case "finally_clause":
			inFinally = true
		}
		if inFinally {
			switch n.Kind() {
			case "function_call_expression":
				v.checkFinallyFunctionCall(n, cleaned)
			case "assignment_expression":
				v.checkFinallyAssignment(n, cleaned)
			case "unset_statement":
				v.checkFinallyUnset(n, cleaned)
			}
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i), inFinally)
		}
	}
	if body := method.ChildByFieldName("body"); body != nil {
		walk(body, false)
	}
	return cleaned
}

// thisWriteTarget returns the property written by $this->prop or $this->prop[...] = ...
func (v *PHPVisitor) thisWriteTarget(n *sitter.Node) (string, bool) {
	for n != nil && n.Kind() == "subscript_expression" && n.ChildCount() > 0 {
		n = n.Child(0)
	}
	if n == nil || n.Kind() != "member_access_expression" {
		return "", false
	}
	if obj := n.ChildByFieldName("object"); obj == nil || v.getContent(obj) != "$this" {
		return "", false
	}
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return "", false
	}
	return v.getContent(nameNode), true
}

// isPublicMethod reports whether a method declaration has no private or protected modifier.
func (v *PHPVisitor) isPublicMethod(method *sitter.Node) bool {
	for i := uint(0); i < method.NamedChildCount(); i++ {
		child := method.NamedChild(i)
		if child.Kind() == "visibility_modifier" {
			return strings.EqualFold(v.getContent(child), "public")
		}
	}
	return true
}

// callableWithoutArguments reports whether every parameter of a method has a default value.
func callableWithoutArguments(method *sitter.Node) bool {
	params := method.ChildByFieldName("parameters")
	if params == nil {
		return true
	}
	for i := uint(0); i < params.NamedChildCount(); i++ {
		param := params.NamedChild(i)
		switch param.Kind() {
		case "simple_parameter", "property_promotion_parameter":
			if param.ChildByFieldName("default_value") == nil {
				return false
			}
		}
	}
	return true
}

// resetInterface is the contract services_resetter calls when a service is tagged kernel.reset.
const resetInterface = `Symfony\Contracts\Service\ResetInterface`

// implementsResetInterface resolves the interfaces a class implements through the file imports,
// so that an alias (use ...\ResetInterface as ResetContract) does not hide the contract.
func (v *PHPVisitor) implementsResetInterface(class *sitter.Node) bool {
	for i := uint(0); i < class.NamedChildCount(); i++ {
		clause := class.NamedChild(i)
		if clause.Kind() != "class_interface_clause" {
			continue
		}
		for j := uint(0); j < clause.NamedChildCount(); j++ {
			if strings.EqualFold(v.resolveFQCN(v.getContent(clause.NamedChild(j))), resetInterface) {
				return true
			}
		}
	}
	return false
}

func intersects(a, b map[string]bool) bool {
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}
