package main

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type mutationInfo struct {
	line int
	code string
}

// PHPVisitor analyzes a single PHP file using tree-sitter.
type PHPVisitor struct {
	content   []byte
	lines     []string
	findings  []Finding
	curClass  string
	namespace string
	curMethod string
	isReset   bool
	mutated   map[string]mutationInfo
	resetted  map[string]bool
	auditor   *Auditor
}

func (v *PHPVisitor) walk(n *sitter.Node) {
	if n == nil {
		return
	}
	nodeType := n.Kind()

	oldClass, oldMethod, oldIsRes := v.curClass, v.curMethod, v.isReset

	switch nodeType {
	case "namespace_definition":
		v.handleNamespace(n)
	case "class_declaration", "trait_declaration", "anonymous_class":
		v.handleClass(n)
	case "method_declaration", "function_definition":
		if nameNode := n.ChildByFieldName("name"); nameNode != nil {
			v.curMethod = v.getContent(nameNode)
		}
	case "assignment_expression", "augmented_assignment_expression":
		v.handleMutation(n.ChildByFieldName("left"))
	case "update_expression":
		v.handleMutation(n)
	case "exit_statement", "exit":
		v.addFinding(n, "Usage of exit/die is forbidden in Worker mode.", "Use Symfony response or exceptions instead.", "ERROR")
	case "function_call_expression":
		v.handleFunctionCall(n)
	case "variable_name":
		v.handleVariable(n)
	case "static_variable_declaration":
		v.addFinding(n, "Usage of local static variable is dangerous in Worker mode.", "Static variables persist across requests.", "ERROR")
	}

	for i := uint(0); i < n.ChildCount(); i++ {
		v.walk(n.Child(i))
	}

	if nodeType == "class_declaration" || nodeType == "trait_declaration" || nodeType == "anonymous_class" {
		if v.isReset {
			v.performResetCheck()
		}
		v.curClass, v.isReset = oldClass, oldIsRes
	} else if nodeType == "method_declaration" || nodeType == "function_definition" {
		v.curMethod = oldMethod
	}
}

func (v *PHPVisitor) handleNamespace(n *sitter.Node) {
	if nameNode := n.ChildByFieldName("name"); nameNode != nil {
		v.namespace = v.getContent(nameNode)
	}
}

func (v *PHPVisitor) handleClass(n *sitter.Node) {
	if nameNode := n.ChildByFieldName("name"); nameNode != nil {
		v.curClass = v.getContent(nameNode)
	} else {
		v.curClass = "AnonymousClass"
	}

	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}
	
	if v.auditor != nil {
		v.auditor.recordClassAudited(fullName)
	}

	classText := strings.ToLower(string(v.content[n.StartByte():n.EndByte()]))
	v.isReset = strings.Contains(classText, "resetinterface") || strings.Contains(classText, "resettableinterface")
	
	v.mutated = make(map[string]mutationInfo)
	v.resetted = make(map[string]bool)
}

func (v *PHPVisitor) handleFunctionCall(n *sitter.Node) {
	nameNode := n.ChildByFieldName("name")
	if nameNode != nil {
		name := strings.ToLower(v.getContent(nameNode))
		if name == "die" || name == "exit" {
			v.addFinding(n, "Usage of exit/die is forbidden in Worker mode.", "Use Symfony response or exceptions instead.", "ERROR")
		}
	}
}

func (v *PHPVisitor) handleVariable(n *sitter.Node) {
	name := v.getContent(n)
	if isSuperglobal(name) {
		v.addFinding(n, fmt.Sprintf("Usage of PHP Superglobal %s is forbidden in Worker mode.", name), "Use Symfony Request object instead.", "ERROR")
	}
}

func isSuperglobal(name string) bool {
	switch name {
	case "$_GET", "$_POST", "$_SESSION", "$_SERVER", "$_FILES", "$_COOKIE", "$_REQUEST", "$_ENV":
		return true
	}
	return false
}

func (v *PHPVisitor) handleMutation(n *sitter.Node) {
	if n == nil || v.curMethod == "__construct" || (v.curMethod == "" && v.curClass != "AnonymousClass" && v.curClass != "") {
		return
	}

	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}

	if v.auditor != nil && (v.auditor.isExplicitlyNonShared(fullName) || v.auditor.isSafeNamespace(fullName)) {
		return
	}

	switch n.Kind() {
	case "member_access_expression":
		v.handleMemberAccess(n)
	case "subscript_expression":
		if n.ChildCount() > 0 {
			v.handleMutation(n.Child(0))
		}
	case "scoped_property_access_expression":
		v.handleScopedAccess(n)
	case "update_expression":
		for i := uint(0); i < n.ChildCount(); i++ {
			c := n.Child(i)
			if c.Kind() != "++" && c.Kind() != "--" {
				v.handleMutation(c)
			}
		}
	}
}

func (v *PHPVisitor) handleMemberAccess(n *sitter.Node) {
	obj := n.ChildByFieldName("object")
	if obj != nil {
		objContent := v.getContent(obj)
		if strings.Contains(objContent, "$this") {
			nameNode := n.ChildByFieldName("name")
			if nameNode != nil {
				v.logMutation(n, v.getContent(nameNode), false)
			}
		} else if obj.Kind() == "member_access_expression" || obj.Kind() == "subscript_expression" {
			v.handleMutation(obj)
		}
	}
}

func (v *PHPVisitor) handleScopedAccess(n *sitter.Node) {
	scope := n.ChildByFieldName("scope")
	if scope != nil {
		s := strings.ToLower(v.getContent(scope))
		if s == "self" || s == "static" {
			nameNode := n.ChildByFieldName("name")
			if nameNode != nil {
				v.logMutation(n, v.getContent(nameNode), true)
			}
		}
	}
}

func (v *PHPVisitor) logMutation(n *sitter.Node, prop string, static bool) {
	key := prop
	if static {
		key = "static::" + prop
	}

	if v.curMethod == "reset" {
		v.resetted[key] = true
	} else if v.isReset {
		v.mutated[key] = mutationInfo{line: int(n.StartPosition().Row) + 1, code: v.lines[n.StartPosition().Row]}
	} else if v.curClass != "" || static {
		msg := fmt.Sprintf("Mutation of state '%s' in %s::%s()", key, v.curClass, v.curMethod)
		v.addFinding(n, msg, "State mutations persist across requests in Worker mode.", "ERROR")
	}
}

func (v *PHPVisitor) performResetCheck() {
	for prop, info := range v.mutated {
		if !v.resetted[prop] {
			v.findings = append(v.findings, Finding{
				Message:     fmt.Sprintf("Property '%s' of %s is mutated but not reset in reset().", prop, v.curClass),
				Severity:    "WARNING",
				Line:        info.line,
				Code:        info.code,
				Remediation: fmt.Sprintf("Add '$this->%s = ...' in the reset() method.", prop),
			})
		}
	}
}

func (v *PHPVisitor) addFinding(n *sitter.Node, msg, hint, severity string) {
	row := int(n.StartPosition().Row)
	v.findings = append(v.findings, Finding{Message: msg, Line: row + 1, Code: v.lines[row], Remediation: hint, Severity: severity})
}

func (v *PHPVisitor) getContent(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return string(v.content[n.StartByte():n.EndByte()])
}
