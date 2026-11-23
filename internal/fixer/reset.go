package fixer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/igor-php/igor-php/pkg/symbol"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

const resetInterfaceFQCN = `Symfony\Contracts\Service\ResetInterface`

type propInfo struct {
	static       bool
	typed        bool
	defaultValue string
}

type methodInfo struct {
	name     string
	node     *sitter.Node
	bootTime bool
}

type classInfo struct {
	node    *sitter.Node
	body    *sitter.Node
	props   map[string]propInfo
	methods map[string]methodInfo
	// writes maps a property key to the methods that assign it directly.
	writes map[string]map[string]bool
	// lateStatic marks static property keys written through static::.
	lateStatic map[string]bool
}

// propertyKey normalizes a finding key: "count" or "static::cache".
func propertyKey(raw string) string {
	if name, ok := strings.CutPrefix(raw, "static::"); ok {
		return "static::" + strings.TrimPrefix(name, "$")
	}
	return raw
}

func (f *fixer) collectClasses() map[string][]*classInfo {
	classes := make(map[string][]*classInfo)
	walk(f.root, func(n *sitter.Node) bool {
		if n.Kind() != "class_declaration" && n.Kind() != "trait_declaration" {
			return true
		}
		if name := n.ChildByFieldName("name"); name != nil {
			classes[f.text(name)] = append(classes[f.text(name)], f.buildClass(n))
		}
		return true
	})
	return classes
}

func (f *fixer) buildClass(n *sitter.Node) *classInfo {
	c := &classInfo{
		node:       n,
		body:       n.ChildByFieldName("body"),
		props:      make(map[string]propInfo),
		methods:    make(map[string]methodInfo),
		writes:     make(map[string]map[string]bool),
		lateStatic: make(map[string]bool),
	}
	if c.body == nil {
		return c
	}
	for i := uint(0); i < c.body.ChildCount(); i++ {
		member := c.body.Child(i)
		switch member.Kind() {
		case "property_declaration":
			f.collectProps(c, member)
		case "method_declaration":
			name := f.text(member.ChildByFieldName("name"))
			c.methods[name] = methodInfo{name: name, node: member, bootTime: f.isBootTimeMethod(member, name)}
			f.collectWrites(c, member, name)
		}
	}
	return c
}

func (f *fixer) collectProps(c *classInfo, decl *sitter.Node) {
	info := propInfo{typed: decl.ChildByFieldName("type") != nil}
	for i := uint(0); i < decl.ChildCount(); i++ {
		if decl.Child(i).Kind() == "static_modifier" {
			info.static = true
		}
	}
	for i := uint(0); i < decl.ChildCount(); i++ {
		element := decl.Child(i)
		if element.Kind() != "property_element" {
			continue
		}
		p := info
		p.defaultValue = f.text(element.ChildByFieldName("default_value"))
		c.props[strings.TrimPrefix(f.text(element.ChildByFieldName("name")), "$")] = p
	}
}

// isBootTimeMethod flags methods that set up a service once rather than per
// request: resetting what they write would break the service.
func (f *fixer) isBootTimeMethod(method *sitter.Node, name string) bool {
	lower := strings.ToLower(name)
	if lower == "__construct" || lower == "warmup" || lower == "boot" {
		return true
	}
	if len(name) > 3 && lower[:3] == "set" && name[3] >= 'A' && name[3] <= 'Z' {
		return true
	}
	for i := uint(0); i < method.ChildCount(); i++ {
		child := method.Child(i)
		if child.Kind() == "attribute_list" && strings.Contains(f.text(child), "Required") {
			return true
		}
	}
	prev := method.PrevSibling()
	return prev != nil && prev.Kind() == "comment" && strings.Contains(f.text(prev), "@required")
}

func (f *fixer) collectWrites(c *classInfo, method *sitter.Node, methodName string) {
	body := method.ChildByFieldName("body")
	walk(body, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "anonymous_class":
			return false
		case "assignment_expression", "augmented_assignment_expression", "reference_assignment_expression":
			f.recordWrite(c, n.ChildByFieldName("left"), methodName)
		case "update_expression":
			f.recordWrite(c, n.ChildByFieldName("argument"), methodName)
		case "unset_statement":
			for i := uint(0); i < n.ChildCount(); i++ {
				f.recordWrite(c, n.Child(i), methodName)
			}
		}
		return true
	})
}

func (f *fixer) recordWrite(c *classInfo, target *sitter.Node, methodName string) {
	for target != nil && target.Kind() == "subscript_expression" {
		target = target.Child(0)
	}
	if target == nil {
		return
	}
	key := ""
	switch target.Kind() {
	case "member_access_expression":
		if f.text(target.ChildByFieldName("object")) == "$this" {
			key = f.text(target.ChildByFieldName("name"))
		}
	case "scoped_property_access_expression":
		scope := strings.ToLower(f.text(target.ChildByFieldName("scope")))
		if scope == "self" || scope == "static" {
			key = "static::" + strings.TrimPrefix(f.text(target.ChildByFieldName("name")), "$")
			if scope == "static" {
				c.lateStatic[key] = true
			}
		}
	}
	if key == "" {
		return
	}
	if c.writes[key] == nil {
		c.writes[key] = make(map[string]bool)
	}
	c.writes[key][methodName] = true
}

// resetStatement returns the statement restoring key to its declared default,
// or the reason it cannot be restored safely.
func (c *classInfo) resetStatement(key string) (string, string) {
	name, isStatic := strings.CutPrefix(key, "static::")
	prop, declared := c.props[name]
	if !declared {
		return "", "property is not declared with a default in this class"
	}
	if prop.static != isStatic {
		return "", "property is accessed with the wrong scope"
	}
	value := prop.defaultValue
	if value == "" {
		if prop.typed {
			return "", "typed property has no default value to restore"
		}
		value = "null"
	}
	writers := c.writes[key]
	if len(writers) == 0 {
		return "", "property holds an object mutated through method calls"
	}
	for writer := range writers {
		if c.methods[writer].bootTime {
			return "", fmt.Sprintf("property is initialized in %s()", writer)
		}
	}
	if !isStatic {
		return fmt.Sprintf("$this->%s = %s;", name, value), ""
	}
	scope := "self"
	if c.lateStatic[key] {
		scope = "static"
	}
	return fmt.Sprintf("%s::$%s = %s;", scope, name, value), ""
}

// findClass resolves a finding's context class to a single class in the file.
func (f *fixer) findClass(contextClass string) *classInfo {
	short := contextClass
	if i := strings.LastIndex(short, `\`); i >= 0 {
		short = short[i+1:]
	}
	matches := f.classes[short]
	if len(matches) != 1 {
		return nil
	}
	return matches[0]
}

// fixIncompleteResets adds the missing assignments to existing reset() methods.
func (f *fixer) fixIncompleteResets(findings []symbol.Finding) {
	type pending struct {
		class *classInfo
		stmts []string
	}
	byClass := make(map[*classInfo]*pending)
	var order []*pending

	for _, finding := range findings {
		if finding.Ignored {
			continue
		}
		match := incompleteResetRe.FindStringSubmatch(finding.Message)
		if match == nil {
			continue
		}
		class := f.findClass(finding.ContextClass)
		if class == nil {
			f.skipped(RuleIncompleteReset, finding.Line, "class could not be identified")
			continue
		}
		if reason := f.resetMethodProblem(class); reason != "" {
			f.skipped(RuleIncompleteReset, finding.Line, reason)
			continue
		}
		key := propertyKey(match[1])
		stmt, reason := class.resetStatement(key)
		if reason != "" {
			f.skipped(RuleIncompleteReset, finding.Line, fmt.Sprintf("'%s': %s", match[1], reason))
			continue
		}
		p := byClass[class]
		if p == nil {
			p = &pending{class: class}
			byClass[class] = p
			order = append(order, p)
		}
		p.stmts = append(p.stmts, stmt)
		f.applied(RuleIncompleteReset, finding.Line, "added `"+stmt+"` to reset()")
	}

	for _, p := range order {
		f.insertIntoReset(p.class, p.stmts)
	}
}

func (f *fixer) resetMethodProblem(c *classInfo) string {
	method, ok := c.methods["reset"]
	if !ok {
		return "reset() is not declared in this class"
	}
	body := method.node.ChildByFieldName("body")
	if body == nil {
		return "reset() has no body"
	}
	hasReturn := false
	walk(body, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "anonymous_function", "arrow_function", "anonymous_class":
			return false
		case "return_statement":
			hasReturn = true
		}
		return true
	})
	if hasReturn {
		return "reset() contains a return statement"
	}
	return ""
}

func (f *fixer) insertIntoReset(c *classInfo, stmts []string) {
	method := c.methods["reset"].node
	body := method.ChildByFieldName("body")
	closing := body.Child(body.ChildCount() - 1)
	methodIndent := f.lineIndent(method.StartByte())
	stmtIndent := methodIndent + indentUnit(methodIndent)

	var block strings.Builder
	for _, stmt := range stmts {
		block.WriteString(stmtIndent + stmt + f.newline)
	}
	if f.onlyWhitespaceBefore(closing.StartByte()) {
		at := f.lineStart(closing.StartByte())
		f.edits = append(f.edits, edit{start: at, end: at, text: block.String()})
		return
	}
	// Single-line body such as `{}`: expand it, keeping any existing statements.
	open := body.Child(0)
	inner := strings.TrimSpace(string(f.content[open.EndByte():closing.StartByte()]))
	text := f.newline
	if inner != "" {
		text += stmtIndent + inner + f.newline
	}
	text += block.String() + f.lineIndent(closing.StartByte())
	f.edits = append(f.edits, edit{start: open.EndByte(), end: closing.StartByte(), text: text})
}

// fixStateMutations makes a class resettable when every property it mutates can
// be restored to its declared default. A class is converted entirely or not at all.
func (f *fixer) fixStateMutations(findings []symbol.Finding) {
	type group struct {
		class    *classInfo
		keys     map[string]bool
		findings []symbol.Finding
	}
	groups := make(map[*classInfo]*group)
	var order []*group

	for _, finding := range findings {
		if finding.Ignored {
			continue
		}
		match := stateMutationRe.FindStringSubmatch(finding.Message)
		if match == nil {
			continue
		}
		class := f.findClass(finding.ContextClass)
		if class == nil {
			f.skipped(RuleStateMutation, finding.Line, "class could not be identified")
			continue
		}
		g := groups[class]
		if g == nil {
			g = &group{class: class, keys: make(map[string]bool)}
			groups[class] = g
			order = append(order, g)
		}
		g.keys[propertyKey(match[1])] = true
		g.findings = append(g.findings, finding)
	}

	for _, g := range order {
		stmts, reason := f.planClassReset(g.class, g.keys)
		if reason != "" {
			for _, finding := range g.findings {
				f.skipped(RuleStateMutation, finding.Line, reason)
			}
			continue
		}
		f.addResetInterface(g.class, stmts)
		for _, finding := range g.findings {
			f.applied(RuleStateMutation, finding.Line, "made the class resettable with a generated reset()")
		}
	}
}

func (f *fixer) planClassReset(c *classInfo, keys map[string]bool) ([]string, string) {
	if !f.opts.HasResetInterface {
		return nil, "Symfony ResetInterface is not installed"
	}
	if c.node.Kind() != "class_declaration" {
		return nil, "traits cannot implement an interface"
	}
	for i := uint(0); i < c.node.ChildCount(); i++ {
		if c.node.Child(i).Kind() == "base_clause" {
			return nil, "class extends a parent that may define its own reset()"
		}
	}
	if _, exists := c.methods["reset"]; exists {
		return nil, "class already has a reset() method"
	}
	if c.body == nil || !f.onlyWhitespaceBefore(c.body.Child(c.body.ChildCount()-1).StartByte()) {
		return nil, "class body is not in a supported layout"
	}
	for key, writers := range c.writes {
		if keys[key] {
			continue
		}
		for writer := range writers {
			if !c.methods[writer].bootTime {
				return nil, fmt.Sprintf("'%s' is mutated but excluded from the audit (baseline or ignore)", key)
			}
		}
	}

	sorted := make([]string, 0, len(keys))
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	stmts := make([]string, 0, len(sorted))
	for _, key := range sorted {
		stmt, reason := c.resetStatement(key)
		if reason != "" {
			return nil, fmt.Sprintf("'%s': %s", key, reason)
		}
		stmts = append(stmts, stmt)
	}
	return stmts, ""
}

func (f *fixer) addResetInterface(c *classInfo, stmts []string) {
	name := f.resolveResetName()

	var clause *sitter.Node
	for i := uint(0); i < c.node.ChildCount(); i++ {
		if c.node.Child(i).Kind() == "class_interface_clause" {
			clause = c.node.Child(i)
		}
	}
	if clause != nil {
		f.edits = append(f.edits, edit{start: clause.EndByte(), end: clause.EndByte(), text: ", " + name})
	} else {
		at := c.node.ChildByFieldName("name").EndByte()
		f.edits = append(f.edits, edit{start: at, end: at, text: " implements " + name})
	}

	closing := c.body.Child(c.body.ChildCount() - 1)
	classIndent := f.lineIndent(closing.StartByte())
	unit := indentUnit(classIndent)
	methodIndent := classIndent + unit

	var method strings.Builder
	if c.body.NamedChildCount() > 0 {
		method.WriteString(f.newline)
	}
	method.WriteString(methodIndent + "public function reset(): void" + f.newline)
	method.WriteString(methodIndent + "{" + f.newline)
	for _, stmt := range stmts {
		method.WriteString(methodIndent + unit + stmt + f.newline)
	}
	method.WriteString(methodIndent + "}" + f.newline)
	at := f.lineStart(closing.StartByte())
	f.edits = append(f.edits, edit{start: at, end: at, text: method.String()})
}

// resolveResetName returns how the file should refer to ResetInterface, adding
// a use statement when that is unambiguous and falling back to the FQCN otherwise.
func (f *fixer) resolveResetName() string {
	if f.resetName != "" {
		return f.resetName
	}
	f.resetName = f.computeResetName()
	return f.resetName
}

func (f *fixer) computeResetName() string {
	fqcn := `\` + resetInterfaceFQCN
	for alias, target := range f.imports {
		if strings.EqualFold(target, resetInterfaceFQCN) {
			return alias
		}
	}
	if f.resetShortNameTaken() {
		return fqcn
	}
	at, prefix, ok := f.importInsertion()
	if !ok {
		return fqcn
	}
	f.edits = append(f.edits, edit{start: at, end: at, text: prefix + "use " + resetInterfaceFQCN + ";"})
	return "ResetInterface"
}

func (f *fixer) resetShortNameTaken() bool {
	for alias := range f.imports {
		if strings.EqualFold(alias, "ResetInterface") {
			return true
		}
	}
	taken := false
	walk(f.root, func(n *sitter.Node) bool {
		switch n.Kind() {
		case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration":
			if strings.EqualFold(f.text(n.ChildByFieldName("name")), "ResetInterface") {
				taken = true
			}
		case "namespace_use_declaration":
			if strings.Contains(f.text(n), "{") && strings.Contains(f.text(n), "ResetInterface") {
				taken = true
			}
		}
		return true
	})
	return taken
}

// importInsertion finds where to add a use statement. It only supports files
// with a single, unbracketed namespace.
func (f *fixer) importInsertion() (uint, string, bool) {
	var namespace, lastUse *sitter.Node
	namespaces := 0
	for i := uint(0); i < f.root.ChildCount(); i++ {
		child := f.root.Child(i)
		switch child.Kind() {
		case "namespace_definition":
			namespaces++
			namespace = child
			if child.ChildByFieldName("body") != nil {
				return 0, "", false
			}
		case "namespace_use_declaration":
			if namespaces > 0 {
				lastUse = child
			}
		case "class_declaration", "interface_declaration", "trait_declaration", "enum_declaration", "function_definition":
			i = f.root.ChildCount()
		}
	}
	if namespaces != 1 {
		return 0, "", false
	}
	if lastUse != nil {
		return lastUse.EndByte(), f.newline, true
	}
	return namespace.EndByte(), f.newline + f.newline, true
}

// collectImports maps the local alias of each class import to its FQCN.
func (f *fixer) collectImports() map[string]string {
	imports := make(map[string]string)
	for i := uint(0); i < f.root.ChildCount(); i++ {
		decl := f.root.Child(i)
		if decl.Kind() != "namespace_use_declaration" || strings.Contains(f.text(decl), "{") {
			continue
		}
		isClassImport := true
		for j := uint(0); j < decl.ChildCount(); j++ {
			if kind := decl.Child(j).Kind(); kind == "function" || kind == "const" {
				isClassImport = false
			}
		}
		if !isClassImport {
			continue
		}
		for j := uint(0); j < decl.ChildCount(); j++ {
			clause := decl.Child(j)
			if clause.Kind() != "namespace_use_clause" {
				continue
			}
			fields := strings.Fields(f.text(clause))
			if len(fields) == 0 {
				continue
			}
			target := strings.TrimPrefix(fields[0], `\`)
			alias := target[strings.LastIndex(target, `\`)+1:]
			if len(fields) == 3 && strings.EqualFold(fields[1], "as") {
				alias = fields[2]
			}
			imports[alias] = target
		}
	}
	return imports
}
