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

type FrankenVisitor struct {
	content       []byte
	lines         []string
	findings      []Finding
	curClass      string
	curMethod     string
	isReset       bool
	mutated       map[string]mutationInfo
	resetted      map[string]bool
}

func (v *FrankenVisitor) walk(n *sitter.Node) {
	if n == nil { return }
	nodeType := n.Kind()
	oldClass, oldMethod, oldIsRes := v.curClass, v.curMethod, v.isReset

	switch nodeType {
	case "class_declaration", "trait_declaration", "anonymous_class":
		nameNode := n.ChildByFieldName("name")
		if nameNode != nil { v.curClass = v.getContent(nameNode) } else { v.curClass = "AnonymousClass" }
		v.isReset = strings.Contains(strings.ToLower(v.getContent(n)), "resetinterface")
		v.mutated, v.resetted = make(map[string]mutationInfo), make(map[string]bool)

	case "method_declaration", "function_definition":
		nameNode := n.ChildByFieldName("name")
		if nameNode != nil { v.curMethod = v.getContent(nameNode) }

	case "assignment_expression", "augmented_assignment_expression":
		v.handleMutation(n.ChildByFieldName("left"), "Mutation")

	case "update_expression":
		v.handleMutation(n, "Mutation")

	case "global_declaration":
		v.addFinding(n, "Usage of 'global' keyword is forbidden.", "Use dependency injection instead.", "ERROR")

	case "static_variable_declaration":
		v.addFinding(n, "Local static variable detected (potential state leak).", "Avoid persistent state in functions.", "ERROR")

	case "exit_statement":
		v.addFinding(n, "Usage of exit/die is forbidden in Worker mode.", "", "ERROR")

	case "function_call_expression":
		nameNode := n.ChildByFieldName("name")
		if nameNode != nil {
			name := strings.ToLower(v.getContent(nameNode))
			if name == "die" || name == "exit" {
				v.addFinding(n, "Usage of exit/die is forbidden in Worker mode.", "", "ERROR")
			}
		}
	}

	for i := uint(0); i < n.ChildCount(); i++ {
		v.walk(n.Child(i))
	}

	if nodeType == "class_declaration" || nodeType == "trait_declaration" || nodeType == "anonymous_class" {
		if v.isReset { v.performResetCheck() }
		v.curClass, v.isReset = oldClass, oldIsRes
	} else if nodeType == "method_declaration" || nodeType == "function_definition" {
		v.curMethod = oldMethod
	}
}

func (v *FrankenVisitor) handleMutation(n *sitter.Node, action string) {
	if n == nil || v.curMethod == "__construct" { return }
	
	switch n.Kind() {
	case "property_element", "member_access_expression":
		obj := n.ChildByFieldName("object")
		if obj != nil && strings.Contains(v.getContent(obj), "this") {
			nameNode := n.ChildByFieldName("name")
			if nameNode != nil && v.curClass != "" { v.logMutation(n, v.getContent(nameNode), false, action) }
		}
	case "scoped_property_access_expression":
		scope := n.ChildByFieldName("scope")
		if scope != nil {
			s := strings.ToLower(v.getContent(scope))
			if s == "self" || s == "static" {
				nameNode := n.ChildByFieldName("name")
				if nameNode != nil { v.logMutation(n, v.getContent(nameNode), true, action) }
			}
		}
	case "subscript_expression":
		v.handleMutation(n.Child(0), action)
	case "array_creation_expression", "list_literal":
		for i := uint(0); i < n.ChildCount(); i++ { v.handleMutation(n.Child(i), action) }
	case "array_item":
		v.handleMutation(n.Child(0), action)
	case "update_expression":
		for i := uint(0); i < n.ChildCount(); i++ {
			c := n.Child(i); if c.Kind() != "++" && c.Kind() != "--" { v.handleMutation(c, action) }
		}
	}
}

func (v *FrankenVisitor) logMutation(n *sitter.Node, prop string, static bool, action string) {
	key := prop; if static { key = "static::" + prop }
	if v.curMethod == "reset" {
		v.resetted[key] = true
	} else if v.isReset {
		v.mutated[key] = mutationInfo{line: int(n.StartPosition().Row) + 1, code: v.lines[n.StartPosition().Row]}
	} else if v.curClass != "" || static {
		v.addFinding(n, fmt.Sprintf("%s of state '%s' in %s.", action, key, v.getScopeName()), "", "ERROR")
	}
}

func (v *FrankenVisitor) getScopeName() string {
	if v.curClass != "" {
		if v.curMethod != "" { return v.curClass + "::" + v.curMethod + "()" }
		return v.curClass
	}
	if v.curMethod != "" { return "function " + v.curMethod + "()" }
	return "global scope"
}

func (v *FrankenVisitor) performResetCheck() {
	for p, info := range v.mutated {
		if !v.resetted[p] {
			v.findings = append(v.findings, Finding{
				Severity: "WARNING", Line: info.line, Code: info.code,
				Message: fmt.Sprintf("Property '%s' of %s is mutated but not reset in reset().", p, v.curClass),
				Remediation: fmt.Sprintf("Add '$this->%s = ...' in the reset() method.", p),
			})
		}
	}
}

func (v *FrankenVisitor) addFinding(n *sitter.Node, msg, hint, severity string) {
	row := int(n.StartPosition().Row)
	v.findings = append(v.findings, Finding{Message: msg, Line: row + 1, Code: v.lines[row], Remediation: hint, Severity: severity})
}

func (v *FrankenVisitor) getContent(n *sitter.Node) string {
	if n == nil { return "" }
	return string(v.content[n.StartByte():n.EndByte()])
}
