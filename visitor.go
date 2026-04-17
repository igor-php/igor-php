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
	content         []byte
	lines           []string
	findings        []Finding
	curClass        string
	namespace       string
	curMethod       string
	isReset         bool
	isReadonlyClass bool
	readonlyProps   map[string]bool
	mutated         map[string]mutationInfo
	resetted        map[string]bool
	auditor         *Auditor
}

func (v *PHPVisitor) walk(n *sitter.Node) {
	if n == nil {
		return
	}
	nodeType := n.Kind()

	oldClass, oldMethod, oldIsRes, oldIsReadonly, oldReadonlyProps := v.curClass, v.curMethod, v.isReset, v.isReadonlyClass, v.readonlyProps

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
		v.curClass, v.isReset, v.isReadonlyClass, v.readonlyProps = oldClass, oldIsRes, oldIsReadonly, oldReadonlyProps
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
	headerEnd := strings.Index(classText, "{")
	if headerEnd == -1 {
		headerEnd = len(classText)
	}
	classHeader := classText[:headerEnd]

	v.isReset = strings.Contains(classHeader, "resetinterface") || strings.Contains(classHeader, "resettableinterface")
	v.isReadonlyClass = strings.Contains(classHeader, "readonly")

	v.mutated = make(map[string]mutationInfo)
	v.resetted = make(map[string]bool)
	v.readonlyProps = make(map[string]bool)

	v.scanReadonlyProps(n)
}

func (v *PHPVisitor) scanReadonlyProps(classNode *sitter.Node) {
	body := classNode.ChildByFieldName("body")
	if body == nil {
		return
	}

	for i := uint(0); i < body.ChildCount(); i++ {
		member := body.Child(i)
		// 1. Regular property declarations
		if member.Kind() == "property_declaration" {
			v.scanPropertyNode(member)
		}
		// 2. Constructor promotion
		if member.Kind() == "method_declaration" {
			nameNode := member.ChildByFieldName("name")
			if nameNode != nil && strings.ToLower(v.getContent(nameNode)) == "__construct" {
				params := member.ChildByFieldName("parameters")
				if params != nil {
					for j := uint(0); j < params.ChildCount(); j++ {
						param := params.Child(j)
						if param.Kind() == "parameter_declaration" || param.Kind() == "property_promotion_parameter" {
							v.scanPropertyNode(param)
						}
					}
				}
			}
		}
	}
}

func (v *PHPVisitor) scanPropertyNode(n *sitter.Node) {
	isReadonly := false
	// Check for readonly modifier
	for j := uint(0); j < n.ChildCount(); j++ {
		child := n.Child(j)
		if (child.Kind() == "modifier" || child.Kind() == "readonly_modifier") && strings.Contains(v.getContent(child), "readonly") {
			isReadonly = true
			break
		}
	}

	if isReadonly {
		// For property_declaration, properties are in property_element
		// For parameter_declaration/property_promotion_parameter, look for variable_name child
		if n.Kind() == "property_declaration" {
			for j := uint(0); j < n.ChildCount(); j++ {
				child := n.Child(j)
				if child.Kind() == "property_element" {
					nameNode := child.ChildByFieldName("name")
					if nameNode != nil {
						v.readonlyProps[strings.TrimPrefix(v.getContent(nameNode), "$")] = true
					}
				}
			}
		} else {
			// Find the variable_name child
			for j := uint(0); j < n.ChildCount(); j++ {
				child := n.Child(j)
				if child.Kind() == "variable_name" {
					v.readonlyProps[strings.TrimPrefix(v.getContent(child), "$")] = true
					break
				}
			}
		}
	}
}

func (v *PHPVisitor) handleFunctionCall(n *sitter.Node) {
	nameNode := n.ChildByFieldName("function")
	if nameNode == nil {
		nameNode = n.ChildByFieldName("name")
	}

	if nameNode == nil {
		return
	}

	name := strings.ToLower(v.getContent(nameNode))
	switch name {
	case "date_default_timezone_set", "ini_set", "setlocale", "error_reporting", "putenv":
		msg := fmt.Sprintf("Function '%s' modifies the global PHP process state.", name)
		hint := "This change will persist across requests in Worker mode and might affect other users."
		v.addFinding(n, msg, hint, "WARNING")
	}
}

func (v *PHPVisitor) handleVariable(n *sitter.Node) {
	name := v.getContent(n)
	if isSuperglobal(name) {
		v.addFinding(n, fmt.Sprintf("Usage of PHP Superglobal %s is discouraged in Worker mode.", name), "Use the Symfony Request object ($request->query, $request->request, etc.) instead.", "WARNING")
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
	if n == nil || v.isReadonlyClass || v.curMethod == "__construct" || (v.curMethod == "" && v.curClass != "AnonymousClass" && v.curClass != "") {
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
	if obj == nil {
		return
	}

	objContent := v.getContent(obj)
	switch {
	case strings.Contains(objContent, "$this"):
		nameNode := n.ChildByFieldName("name")
		if nameNode != nil {
			v.logMutation(n, v.getContent(nameNode), false)
		}
	case obj.Kind() == "member_access_expression" || obj.Kind() == "subscript_expression":
		v.handleMutation(obj)
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

	// Skip if property is readonly
	if !static && v.readonlyProps[prop] {
		return
	}

	switch {
	case v.curMethod == "reset":
		v.resetted[key] = true
	case v.isReset:
		v.mutated[key] = mutationInfo{line: int(n.StartPosition().Row) + 1, code: v.lines[n.StartPosition().Row]}
	case v.curClass != "" || static:
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
	lineContent := v.lines[row]

	// Check if the current line or the previous line contains @igor-ignore
	if strings.Contains(lineContent, "@igor-ignore") {
		return
	}
	if row > 0 && strings.Contains(v.lines[row-1], "@igor-ignore") {
		return
	}

	v.findings = append(v.findings, Finding{Message: msg, Line: row + 1, Code: v.lines[row], Remediation: hint, Severity: severity})
}

func (v *PHPVisitor) getContent(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return string(v.content[n.StartByte():n.EndByte()])
}
