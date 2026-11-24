package fixer

import (
	"fmt"
	"strings"

	"github.com/igor-php/igor-php/pkg/symbol"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

const requestFQCN = `Symfony\Component\HttpFoundation\Request`

var superglobalBags = map[string]string{
	"$_GET":    "query",
	"$_POST":   "request",
	"$_COOKIE": "cookies",
	"$_SERVER": "server",
}

// byValueFunctions are functions known not to take their argument by reference,
// so a superglobal read passed to them can be rewritten.
var byValueFunctions = map[string]bool{
	"isset": true, "empty": true, "trim": true, "ltrim": true, "rtrim": true,
	"intval": true, "floatval": true, "strval": true, "boolval": true,
	"strtolower": true, "strtoupper": true, "strlen": true, "htmlspecialchars": true,
	"is_numeric": true, "is_string": true, "is_array": true, "count": true,
}

// fixSuperglobals rewrites read-only superglobal accesses to the Request in scope.
func (f *fixer) fixSuperglobals(findings []symbol.Finding) {
	occurrences := make(map[string][]*sitter.Node)
	walk(f.root, func(n *sitter.Node) bool {
		if n.Kind() == "variable_name" {
			key := fmt.Sprintf("%d:%s", n.StartPosition().Row+1, f.text(n))
			occurrences[key] = append(occurrences[key], n)
			return false
		}
		return true
	})

	for _, finding := range findings {
		if finding.Ignored {
			continue
		}
		match := superglobalRe.FindStringSubmatch(finding.Message)
		if match == nil {
			continue
		}
		name := match[1]
		bag, supported := superglobalBags[name]
		if !supported {
			f.skipped(RuleSuperglobals, finding.Line, name+" has no direct Request equivalent")
			continue
		}
		key := fmt.Sprintf("%d:%s", finding.Line, name)
		if len(occurrences[key]) == 0 {
			f.skipped(RuleSuperglobals, finding.Line, "occurrence could not be located")
			continue
		}
		node := occurrences[key][0]
		occurrences[key] = occurrences[key][1:]

		if reason := f.superglobalReadProblem(node); reason != "" {
			f.skipped(RuleSuperglobals, finding.Line, reason)
			continue
		}
		requestVar, note := f.requestInScope(node), ""
		if requestVar == "" {
			var reason string
			requestVar, note, reason = f.injectRequest(node)
			if reason != "" {
				f.skipped(RuleSuperglobals, finding.Line, reason)
				continue
			}
		}
		replacement := fmt.Sprintf("%s->%s->all()", requestVar, bag)
		f.edits = append(f.edits, edit{start: node.StartByte(), end: node.EndByte(), text: replacement})
		f.applied(RuleSuperglobals, finding.Line, fmt.Sprintf("%sreplaced %s with %s", note, name, replacement))
	}
}

// superglobalReadProblem returns why the access is not a plain subscripted read.
func (f *fixer) superglobalReadProblem(node *sitter.Node) string {
	parent := node.Parent()
	if parent == nil || parent.Kind() != "subscript_expression" || !sameNode(parent.Child(0), node) || parent.NamedChildCount() < 2 {
		return "only subscripted reads can be rewritten"
	}

	outer := parent
	for up := outer.Parent(); up != nil && up.Kind() == "subscript_expression" && sameNode(up.Child(0), outer); up = outer.Parent() {
		outer = up
	}

	if reason := writeContextProblem(outer); reason != "" {
		return reason
	}

	if up := outer.Parent(); up != nil && up.Kind() == "argument" {
		call := up.Parent()
		if call != nil {
			call = call.Parent()
		}
		if call == nil || call.Kind() != "function_call_expression" ||
			!byValueFunctions[strings.ToLower(strings.TrimPrefix(f.text(call.ChildByFieldName("function")), `\`))] {
			return "argument may be passed by reference"
		}
	}
	return ""
}

// writeContextProblem walks up to the enclosing statement looking for a write.
func writeContextProblem(expr *sitter.Node) string {
	for a := expr.Parent(); a != nil; a = a.Parent() {
		switch a.Kind() {
		case "assignment_expression", "augmented_assignment_expression":
			if contains(a.ChildByFieldName("left"), expr) {
				return "superglobal is written to"
			}
		case "reference_assignment_expression", "by_ref", "update_expression", "unset_statement":
			return "superglobal is written to or taken by reference"
		case "foreach_statement":
			return "superglobal is iterated and may be taken by reference"
		case "expression_statement", "compound_statement":
			return ""
		}
	}
	return ""
}

// requestInScope returns the variable holding a Symfony Request at node, if any.
func (f *fixer) requestInScope(node *sitter.Node) string {
	for a := node.Parent(); a != nil; a = a.Parent() {
		switch a.Kind() {
		case "arrow_function":
			if name := f.requestParameter(a); name != "" {
				return name
			}
		case "method_declaration", "function_definition", "anonymous_function":
			return f.requestParameter(a)
		}
	}
	return ""
}

func (f *fixer) requestParameter(fn *sitter.Node) string {
	params := fn.ChildByFieldName("parameters")
	if params == nil {
		return ""
	}
	for i := uint(0); i < params.NamedChildCount(); i++ {
		param := params.NamedChild(i)
		if param.Kind() != "simple_parameter" {
			continue
		}
		typeNode := param.ChildByFieldName("type")
		if typeNode == nil || typeNode.Kind() != "named_type" {
			continue
		}
		if f.resolvesToRequest(f.text(typeNode)) {
			return f.text(param.ChildByFieldName("name"))
		}
	}
	return ""
}

func (f *fixer) resolvesToRequest(typeName string) bool {
	if fq, ok := strings.CutPrefix(typeName, `\`); ok {
		return strings.EqualFold(fq, requestFQCN)
	}
	return strings.EqualFold(f.imports[typeName], requestFQCN)
}

// fixGCDisable removes standalone gc_disable() calls with no matching gc_enable().
func (f *fixer) fixGCDisable(findings []symbol.Finding) {
	var calls []*sitter.Node
	hasEnable := false
	walk(f.root, func(n *sitter.Node) bool {
		if n.Kind() == "function_call_expression" {
			switch strings.ToLower(strings.TrimPrefix(f.text(n.ChildByFieldName("function")), `\`)) {
			case "gc_enable":
				hasEnable = true
			case "gc_disable":
				calls = append(calls, n)
			}
		}
		return true
	})

	for _, finding := range findings {
		if finding.Ignored || !strings.HasPrefix(finding.Message, gcDisablePrefix) {
			continue
		}
		if hasEnable {
			f.skipped(RuleProcessState, finding.Line, "file also calls gc_enable()")
			continue
		}
		var call *sitter.Node
		for i, candidate := range calls {
			if int(candidate.StartPosition().Row)+1 == finding.Line {
				call = candidate
				calls = append(calls[:i], calls[i+1:]...)
				break
			}
		}
		if call == nil {
			f.skipped(RuleProcessState, finding.Line, "occurrence could not be located")
			continue
		}
		stmt := call.Parent()
		if stmt == nil || stmt.Kind() != "expression_statement" || stmt.Parent() == nil ||
			(stmt.Parent().Kind() != "compound_statement" && stmt.Parent().Kind() != "program") {
			f.skipped(RuleProcessState, finding.Line, "gc_disable() is not a standalone statement")
			continue
		}
		start, end := f.statementRange(stmt)
		f.edits = append(f.edits, edit{start: start, end: end})
		f.applied(RuleProcessState, finding.Line, "removed gc_disable()")
	}
}

// statementRange extends a statement to its whole line when nothing else is on it.
func (f *fixer) statementRange(stmt *sitter.Node) (uint, uint) {
	start, end := stmt.StartByte(), stmt.EndByte()
	if !f.onlyWhitespaceBefore(start) {
		return start, end
	}
	cursor := end
	for cursor < uint(len(f.content)) && (f.content[cursor] == ' ' || f.content[cursor] == '\t' || f.content[cursor] == '\r') {
		cursor++
	}
	if cursor >= uint(len(f.content)) || f.content[cursor] != '\n' {
		return start, end
	}
	lineStart, lineEnd := f.lineStart(start), cursor+1
	if f.followsOpeningBrace(lineStart) {
		lineEnd = f.skipBlankLine(lineEnd)
	}
	return lineStart, lineEnd
}

// followsOpeningBrace reports whether the line before lineStart ends with "{".
func (f *fixer) followsOpeningBrace(lineStart uint) bool {
	if lineStart == 0 {
		return false
	}
	previous := strings.TrimRight(string(f.content[f.lineStart(lineStart-1):lineStart]), " \t\r\n")
	return strings.HasSuffix(previous, "{")
}

// skipBlankLine returns the start of the next line if the line at pos is blank.
func (f *fixer) skipBlankLine(pos uint) uint {
	cursor := pos
	for cursor < uint(len(f.content)) && (f.content[cursor] == ' ' || f.content[cursor] == '\t' || f.content[cursor] == '\r') {
		cursor++
	}
	if cursor < uint(len(f.content)) && f.content[cursor] == '\n' {
		return cursor + 1
	}
	return pos
}

var routeAttributeFQCNs = map[string]bool{
	`symfony\component\routing\attribute\route`:  true,
	`symfony\component\routing\annotation\route`: true,
}

// injectRequest adds a Request parameter to the Symfony route action enclosing
// node, which Symfony's argument resolver then fills in. It returns the variable
// name, a note describing the change, or the reason it cannot be done.
func (f *fixer) injectRequest(node *sitter.Node) (string, string, string) {
	method := enclosingMethod(node)
	if method == nil {
		return "", "", "no Symfony Request parameter in scope"
	}
	if name, done := f.injected[method.StartByte()]; done {
		return name, "", ""
	}
	name := f.text(method.ChildByFieldName("name"))
	if reason := f.injectionProblem(method, name); reason != "" {
		return "", "", reason
	}

	params := method.ChildByFieldName("parameters")
	decl := f.resolveImport(requestFQCN) + " $request"
	if first := params.NamedChild(0); first == nil {
		at := params.Child(0).EndByte()
		f.edits = append(f.edits, edit{start: at, end: at, text: decl})
	} else if first.StartPosition().Row == params.StartPosition().Row {
		f.edits = append(f.edits, edit{start: first.StartByte(), end: first.StartByte(), text: decl + ", "})
	} else {
		text := decl + "," + f.newline + f.lineIndent(first.StartByte())
		f.edits = append(f.edits, edit{start: first.StartByte(), end: first.StartByte(), text: text})
	}
	f.injected[method.StartByte()] = "$request"
	return "$request", fmt.Sprintf("added `%s` to %s(), ", decl, name), ""
}

// enclosingMethod returns the method whose own scope contains node. Closures
// have their own scope; arrow functions share their parent's.
func enclosingMethod(node *sitter.Node) *sitter.Node {
	for a := node.Parent(); a != nil; a = a.Parent() {
		switch a.Kind() {
		case "method_declaration":
			return a
		case "anonymous_function", "function_definition":
			return nil
		}
	}
	return nil
}

func (f *fixer) injectionProblem(method *sitter.Node, name string) string {
	if !f.isRouteAction(method) {
		return "no Symfony Request parameter in scope and not a #[Route] action"
	}
	for i := uint(0); i < method.ChildCount(); i++ {
		if method.Child(i).Kind() == "static_modifier" {
			return "route action is static"
		}
	}
	if method.ChildByFieldName("parameters") == nil || method.ChildByFieldName("body") == nil {
		return "route action has no parameter list or body"
	}
	if f.isCalledDirectly(name) {
		return fmt.Sprintf("%s() is also called directly in this file", name)
	}
	used := false
	walk(method, func(n *sitter.Node) bool {
		if n.Kind() == "variable_name" && f.text(n) == "$request" {
			used = true
		}
		return !used
	})
	if used {
		return "a $request variable already exists in the action"
	}
	return ""
}

// isRouteAction reports whether method carries Symfony's #[Route] attribute.
func (f *fixer) isRouteAction(method *sitter.Node) bool {
	found := false
	for i := uint(0); i < method.ChildCount(); i++ {
		list := method.Child(i)
		if list.Kind() != "attribute_list" {
			continue
		}
		walk(list, func(n *sitter.Node) bool {
			if n.Kind() != "attribute" {
				return true
			}
			if nameNode := n.NamedChild(0); nameNode != nil && routeAttributeFQCNs[strings.ToLower(f.resolveName(f.text(nameNode)))] {
				found = true
			}
			return false
		})
	}
	return found
}

// resolveName resolves a class reference through the file's imports.
func (f *fixer) resolveName(name string) string {
	if fq, ok := strings.CutPrefix(name, `\`); ok {
		return fq
	}
	if target, ok := f.imports[name]; ok {
		return target
	}
	return ""
}

// isCalledDirectly reports whether the file calls the method, or names it in a
// string such as a [$this, 'method'] callable, which a new parameter would break.
func (f *fixer) isCalledDirectly(name string) bool {
	called := false
	walk(f.root, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "member_call_expression", "nullsafe_member_call_expression", "scoped_call_expression":
			if strings.EqualFold(f.text(n.ChildByFieldName("name")), name) {
				called = true
			}
		case "string_content":
			if strings.EqualFold(f.text(n), name) {
				called = true
			}
		}
		return !called
	})
	return called
}
