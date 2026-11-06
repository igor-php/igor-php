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
func (v *PHPVisitor) checkMissingResetTag(n *sitter.Node, fullName, classHeader string) {
	if v.engine == nil || !v.engine.MissingResetTag(fullName) {
		return
	}
	body := v.getClassBody(n)
	if body == nil {
		return
	}

	cleared := make(map[string]map[string]bool) // reset method (lowercase) => properties it rewrites
	written := make(map[string]bool)            // properties mutated by the other methods
	resetMethods := make(map[string]string)     // lowercase => declared name
	for i := uint(0); i < body.NamedChildCount(); i++ {
		member := body.NamedChild(i)
		if member.Kind() != "method_declaration" {
			continue
		}
		nameNode := member.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		name := v.getContent(nameNode)
		lower := strings.ToLower(name)
		switch {
		case (lower == "reset" || lower == "clear") && v.isPublicMethod(member):
			resetMethods[lower] = name
			cleared[lower] = make(map[string]bool)
			v.collectThisWrites(member, cleared[lower])
		case lower != "__construct":
			v.collectThisWrites(member, written)
		}
	}

	method := ""
	if implementsReset := strings.Contains(classHeader, "resetinterface"); implementsReset && resetMethods["reset"] != "" {
		method = resetMethods["reset"]
	} else {
		for _, candidate := range []string{"reset", "clear"} {
			if resetMethods[candidate] != "" && intersects(cleared[candidate], written) {
				method = resetMethods[candidate]
				break
			}
		}
	}
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

// collectThisWrites records the $this properties a method assigns, appends to or increments.
func (v *PHPVisitor) collectThisWrites(n *sitter.Node, props map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "assignment_expression", "augmented_assignment_expression":
		if prop, ok := v.thisWriteTarget(n.ChildByFieldName("left")); ok {
			props[prop] = true
		}
	case "update_expression":
		for i := uint(0); i < n.NamedChildCount(); i++ {
			if prop, ok := v.thisWriteTarget(n.NamedChild(i)); ok {
				props[prop] = true
			}
		}
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		v.collectThisWrites(n.Child(i), props)
	}
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

func intersects(a, b map[string]bool) bool {
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}
