package analyzer

import (
	"fmt"
	"slices"
	"strings"

	"github.com/igor-php/igor-php/pkg/symbol"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Engine defines the required interface for the auditor to interact with the visitor.
type Engine interface {
	RecordClassAudited(name string)
	IsExplicitlyNonShared(className string) bool
	IsSafeNamespace(className string) bool
	IsResettable(className string) bool
	HasServiceDefinition(className string) bool
	MissingResetTag(className string) bool
	GetMethodReturnType(className, methodName string) string
	IsSharedService(className string) bool
	RecordMethodCall(callerClass, callerMethod, calleeClass, calleeMethod string)
	RecordClassParent(className, parentClassName string)
	RecordMethodDeclared(className, methodName string)
}

type mutationInfo struct {
	method     string   // the method of the reported write
	methods    []string // every method that mutates the property, for reachability
	line       int
	code       string
	snippet    string
	astDetails string
}

// PHPVisitor analyzes a single PHP file using tree-sitter.
type PHPVisitor struct {
	content            []byte
	lines              []string
	findings           []symbol.Finding
	curClass           string
	namespace          string
	curMethod          string
	isReset            bool
	isReadonlyClass    bool
	isWorkerSafeClass  bool
	isWorkerSafeMethod bool
	readonlyProps      map[string]bool
	workerSafeProps    map[string]bool
	propertyTypes      map[string]string
	localVarTypes      map[string]string
	declaredProps      map[string]bool
	finallyCleaned     map[string]bool
	mutated            map[string]mutationInfo
	resetted           map[string]bool
	engine             Engine
	dependencies       []string
	nonSharedServices  NonSharedServiceMap
	localSharedVars    map[string]bool
	// localWrapperVars are locals holding a new object built around shared state (clone, new,
	// collect()): the local itself is fresh, the objects reached through it may not be.
	localWrapperVars map[string]bool
	namespaceScopes  []namespaceScope
	framework        string
}

type namespaceScope struct {
	startByte uint
	endByte   uint
	shadowed  map[string]bool
}

// NewVisitor creates a new instance of the PHPVisitor.
func NewVisitor(content []byte, engine Engine) *PHPVisitor {
	return &PHPVisitor{
		content:          content,
		lines:            strings.Split(string(content), "\n"),
		mutated:          make(map[string]mutationInfo),
		resetted:         make(map[string]bool),
		readonlyProps:    make(map[string]bool),
		workerSafeProps:  make(map[string]bool),
		propertyTypes:    make(map[string]string),
		localVarTypes:    make(map[string]string),
		declaredProps:    make(map[string]bool),
		finallyCleaned:   make(map[string]bool),
		localSharedVars:  make(map[string]bool),
		localWrapperVars: make(map[string]bool),
		engine:           engine,
	}
}

func (v *PHPVisitor) SetDependencies(deps []string) {
	v.dependencies = deps
}

// SetNonSharedServices supplies the generic container-dump lookup of transient
// (shared: false) classes. Mutations inside such classes are skipped, mirroring
// the Symfony bridge's IsExplicitlyNonShared() signal for non-Symfony frameworks.
func (v *PHPVisitor) SetNonSharedServices(m NonSharedServiceMap) {
	v.nonSharedServices = m
}

func (v *PHPVisitor) Walk(n *sitter.Node) {
	v.collectNamespaceScopes(n)
	v.walk(n)
}

func (v *PHPVisitor) Findings() []symbol.Finding {
	return v.findings
}

func (v *PHPVisitor) walk(n *sitter.Node) {
	if n == nil {
		return
	}
	nodeType := n.Kind()

	oldClass, oldMethod, oldIsRes, oldIsReadonly, oldReadonlyProps, oldDeclaredProps, oldIsWorkerSafeClass, oldIsWorkerSafeMethod, oldFinallyCleaned := v.curClass, v.curMethod, v.isReset, v.isReadonlyClass, v.readonlyProps, v.declaredProps, v.isWorkerSafeClass, v.isWorkerSafeMethod, v.finallyCleaned

	switch nodeType {
	case "namespace_definition":
		v.handleNamespace(n)
	case "class_declaration", "trait_declaration", "anonymous_class":
		v.handleClass(n)
	case "method_declaration", "function_definition":
		v.handleMethodOrFunction(n, nodeType)
	case "assignment_expression", "augmented_assignment_expression":
		v.handleAssignment(n)
	case "update_expression", "foreach_statement":
		v.handleMutation(n)
	case "unset_statement":
		v.handleUnset(n)
	case "exit_statement":
		// Not "exit" too: it is the anonymous keyword token inside exit_statement
		v.addExitFinding(n)
	case "name":
		v.handleBareExit(n)
	case "function_call_expression":
		v.handleFunctionCall(n)
	case "member_call_expression":
		v.handleMethodCall(n)
	case "variable_name":
		v.handleVariable(n)
	case "static_variable_declaration":
		// A closure returned to the caller gets its own static variables each time it is created
		if !inReturnedClosure(n) {
			v.addFinding(n, symbol.RuleLocalStaticVariable, "Usage of local static variable is dangerous in Worker mode.", "Static variables persist across requests.", "ERROR")
		}
	}

	for i := uint(0); i < n.ChildCount(); i++ {
		v.walk(n.Child(i))
	}

	switch nodeType {
	case "class_declaration", "trait_declaration", "anonymous_class":
		if v.isReset {
			v.performResetCheck()
		}
		v.curClass, v.isReset, v.isReadonlyClass, v.readonlyProps, v.declaredProps, v.isWorkerSafeClass = oldClass, oldIsRes, oldIsReadonly, oldReadonlyProps, oldDeclaredProps, oldIsWorkerSafeClass
	case "method_declaration", "function_definition":
		v.curMethod, v.isWorkerSafeMethod, v.finallyCleaned = oldMethod, oldIsWorkerSafeMethod, oldFinallyCleaned
	}
}

func (v *PHPVisitor) recordMethodDeclaration(nodeType string) {
	if nodeType != "method_declaration" || v.curClass == "" || v.curMethod == "" || v.engine == nil {
		return
	}

	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}
	v.engine.RecordMethodDeclared(fullName, v.curMethod)
}

func (v *PHPVisitor) handleMethodOrFunction(n *sitter.Node, nodeType string) {
	if nameNode := n.ChildByFieldName("name"); nameNode != nil {
		v.curMethod = v.getContent(nameNode)
	}
	v.isWorkerSafeMethod = v.hasAttribute(n, "WorkerSafe")
	v.finallyCleaned = make(map[string]bool)
	v.localSharedVars = make(map[string]bool)
	v.localWrapperVars = make(map[string]bool)
	v.preScanFinallyCleanups(n)
	v.recordMethodDeclaration(nodeType)
	if nodeType == "method_declaration" {
		v.checkUnsafeRestores(n)
		if v.engine != nil {
			v.recordMethodState(n)
		}
	}

	if nodeType == "method_declaration" && strings.ToLower(v.curMethod) == "__destruct" {
		fullName := v.curClass
		if v.namespace != "" {
			fullName = v.namespace + "\\" + v.curClass
		}
		isTransient := false
		if v.nonSharedServices[strings.TrimPrefix(fullName, "\\")] {
			isTransient = true
		} else if v.engine != nil && (v.engine.IsExplicitlyNonShared(fullName) || v.engine.IsSafeNamespace(fullName)) {
			isTransient = true
		}
		if !isTransient {
			v.addFinding(n, symbol.RuleMagicMethodDestruct, "Usage of __destruct() magic method is dangerous in Worker mode.", "Destructors on shared services are not invoked at the end of each request in Worker mode.", "ERROR")
		}
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

	if v.engine != nil {
		v.engine.RecordClassAudited(fullName)
	}

	if n.Kind() == "class_declaration" {
		v.recordClassParent(n, fullName)
		if v.engine != nil {
			v.recordClassShape(n, fullName)
		}
	}

	classText := strings.ToLower(string(v.content[n.StartByte():n.EndByte()]))
	headerEnd := strings.Index(classText, "{")
	if headerEnd == -1 {
		headerEnd = len(classText)
	}
	classHeader := classText[:headerEnd]

	// The service map says what the container resets between requests: implementing
	// ResetInterface only counts for classes it does not describe (parents, no bundle)
	if v.engine != nil && v.engine.HasServiceDefinition(fullName) {
		v.isReset = v.engine.IsResettable(fullName)
	} else {
		v.isReset = strings.Contains(classHeader, "resetinterface") || strings.Contains(classHeader, "resettableinterface")
		if !v.isReset && v.engine != nil {
			v.isReset = v.engine.IsResettable(fullName)
		}
	}
	v.isReadonlyClass = strings.Contains(classHeader, "readonly")

	v.mutated = make(map[string]mutationInfo)
	v.resetted = make(map[string]bool)
	v.readonlyProps = make(map[string]bool)
	v.workerSafeProps = make(map[string]bool)
	v.propertyTypes = make(map[string]string)
	v.declaredProps = make(map[string]bool)
	v.isWorkerSafeClass = v.hasAttribute(n, "WorkerSafe")

	v.scanReadonlyProps(n)
	v.scanPropertyTypes(n)
	// Not traits: whether their properties persist depends on the classes using them,
	// which can be controllers or other per-request services
	if n.Kind() == "class_declaration" {
		v.checkStaleDependencies(n, fullName)
		v.checkMissingResetTag(n, fullName)
		v.checkDoctrineFlushState(n)
	}
}

// recordClassParent extracts the single `extends` target from a class_declaration's
// base_clause child (if any) and reports it to the engine, resolving the name
// through the same use-import/namespace logic as property type-hints.
func (v *PHPVisitor) recordClassParent(n *sitter.Node, fullName string) {
	if v.engine == nil {
		return
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		child := n.Child(i)
		if child.Kind() != "base_clause" {
			continue
		}
		if nameNode := child.NamedChild(0); nameNode != nil {
			v.engine.RecordClassParent(fullName, v.resolveFQCN(v.getContent(nameNode)))
		}
		return
	}
}

func (v *PHPVisitor) getClassBody(classNode *sitter.Node) *sitter.Node {
	if classNode == nil {
		return nil
	}
	body := classNode.ChildByFieldName("body")
	if body == nil {
		for i := 0; i < int(classNode.ChildCount()); i++ {
			child := classNode.Child(uint(i))
			if child.Kind() == "declaration_list" {
				return child
			}
		}
	}
	return body
}

func (v *PHPVisitor) scanReadonlyProps(classNode *sitter.Node) {
	body := v.getClassBody(classNode)
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
				if params == nil {
					for i := 0; i < int(member.ChildCount()); i++ {
						child := member.Child(uint(i))
						if child.Kind() == "formal_parameters" {
							params = child
							break
						}
					}
				}
				if params != nil {
					for j := uint(0); j < params.ChildCount(); j++ {
						param := params.Child(j)
						if param.Kind() == "parameter_declaration" || param.Kind() == "property_promotion_parameter" || param.Kind() == "simple_parameter" {
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

	isWorkerSafe := v.hasAttribute(n, "WorkerSafe")

	if isReadonly || isWorkerSafe {
		// For property_declaration, properties are in property_element
		// For parameter_declaration/property_promotion_parameter, look for variable_name child
		if n.Kind() == "property_declaration" {
			for j := uint(0); j < n.ChildCount(); j++ {
				child := n.Child(j)
				if child.Kind() == "property_element" {
					nameNode := child.ChildByFieldName("name")
					if nameNode != nil {
						propName := strings.TrimPrefix(v.getContent(nameNode), "$")
						if isReadonly {
							v.readonlyProps[propName] = true
						}
						if isWorkerSafe {
							v.workerSafeProps[propName] = true
						}
						v.declaredProps[propName] = true
					}
				}
			}
		} else {
			// Find the variable_name child
			for j := uint(0); j < n.ChildCount(); j++ {
				child := n.Child(j)
				if child.Kind() == "variable_name" {
					propName := strings.TrimPrefix(v.getContent(child), "$")
					if isReadonly {
						v.readonlyProps[propName] = true
					}
					if isWorkerSafe {
						v.workerSafeProps[propName] = true
					}
					v.declaredProps[propName] = true
					break
				}
			}
		}
	}
}

func (v *PHPVisitor) handleUnset(n *sitter.Node) {
	if v.curMethod != "reset" {
		return
	}

	for i := uint(0); i < n.ChildCount(); i++ {
		child := n.Child(i)
		if child.Kind() == "member_access_expression" {
			obj := child.ChildByFieldName("object")
			if obj != nil && strings.Contains(v.getContent(obj), "$this") {
				nameNode := child.ChildByFieldName("name")
				if nameNode != nil {
					prop := v.getContent(nameNode)
					v.resetted[prop] = true
				}
			}
		}
	}
}

type dangerousFuncRule struct {
	msg       string
	hint      string
	needsArgs bool
}

var dangerousFunctions = map[string]dangerousFuncRule{
	"date_default_timezone_set": {
		msg:  "Function 'date_default_timezone_set' modifies the global PHP process state.",
		hint: "This change will persist across requests in Worker mode and might affect other users.",
	},
	"ini_set": {
		msg:  "Function 'ini_set' modifies the global PHP process state.",
		hint: "This change will persist across requests in Worker mode and might affect other users.",
	},
	"setlocale": {
		msg:  "Function 'setlocale' modifies the global PHP process state.",
		hint: "This change will persist across requests in Worker mode and might affect other users.",
	},
	"error_reporting": {
		msg:  "Function 'error_reporting' modifies the global PHP process state.",
		hint: "This change will persist across requests in Worker mode and might affect other users.",
	},
	"putenv": {
		msg:  "Function 'putenv' modifies the global PHP process state.",
		hint: "This change will persist across requests in Worker mode and might affect other users.",
	},
	"chdir": {
		msg:  "Function 'chdir' modifies the global PHP process working directory.",
		hint: "Changing working directory persists across requests in Worker mode. Use absolute paths instead of chdir().",
	},
	"umask": {
		msg:       "Function 'umask' modifies the global PHP process file creation mask.",
		hint:      "File creation mask changes persist across requests in Worker mode. Explicitly set file permissions with chmod() instead.",
		needsArgs: true,
	},
	"mb_internal_encoding": {
		msg:       "Function 'mb_internal_encoding' modifies the global PHP process multibyte encoding.",
		hint:      "Encoding changes persist across requests in Worker mode. Pass explicit encoding parameters to mb_* functions instead.",
		needsArgs: true,
	},
	"mb_regex_encoding": {
		msg:       "Function 'mb_regex_encoding' modifies the global PHP process multibyte regex encoding.",
		hint:      "Encoding changes persist across requests in Worker mode. Pass explicit encoding parameters to mb_ereg_* functions instead.",
		needsArgs: true,
	},
	"bcscale": {
		msg:       "Function 'bcscale' modifies the global BCMath default precision.",
		hint:      "Default precision changes persist across requests in Worker mode. Pass explicit scale parameters to bc* functions instead.",
		needsArgs: true,
	},
	"set_error_handler": {
		msg:  "Function 'set_error_handler' modifies global process error handlers.",
		hint: "Custom error handlers persist across requests in Worker mode and bypass framework error handling. Rely on Symfony error listeners instead.",
	},
	"set_exception_handler": {
		msg:  "Function 'set_exception_handler' modifies global process exception handlers.",
		hint: "Custom exception handlers persist across requests in Worker mode and bypass framework error handling. Rely on Symfony exception listeners instead.",
	},
	"register_shutdown_function": {
		msg:  "Function 'register_shutdown_function' registers a process-level shutdown handler.",
		hint: "Shutdown functions are deferred until worker process termination or accumulate across requests in Worker mode. Use Symfony kernel terminate events instead.",
	},
	"gc_disable": {
		msg:  "Function 'gc_disable' modifies the global PHP garbage collector.",
		hint: "Disabling the cyclic garbage collector will cause memory to accumulate indefinitely in Worker mode. Keep GC enabled or run gc_collect_cycles() explicitly.",
	},
	"header": {
		msg:  "Function 'header' directly mutates SAPI HTTP output headers.",
		hint: "Direct header output bypasses Symfony Response lifecycle and leaks into SAPI stream in Worker mode. Use Symfony Response object headers instead.",
	},
	"http_response_code": {
		msg:       "Function 'http_response_code' directly mutates SAPI HTTP response status.",
		hint:      "Direct response code changes bypass Symfony Response lifecycle and persist across requests in Worker mode. Use Symfony Response object status code instead.",
		needsArgs: true,
	},
	"session_start": {
		msg:  "Function 'session_start' directly manipulates native PHP session state.",
		hint: "Native session functions bypass Symfony Session management and leak session locks or data across requests in Worker mode. Inject Symfony's Request::getSession() instead.",
	},
	"session_destroy": {
		msg:  "Function 'session_destroy' directly manipulates native PHP session state.",
		hint: "Native session functions bypass Symfony Session management and leak session locks or data across requests in Worker mode. Inject Symfony's Request::getSession() instead.",
	},
	"session_id": {
		msg:       "Function 'session_id' directly manipulates native PHP session state.",
		hint:      "Native session functions bypass Symfony Session management and leak session locks or data across requests in Worker mode. Inject Symfony's Request::getSession() instead.",
		needsArgs: true,
	},
}

func (v *PHPVisitor) addExitFinding(n *sitter.Node) {
	v.addFinding(n, symbol.RuleExecutionTerminator, "Usage of exit/die is forbidden in Worker mode.", v.hint("exit", "Use Symfony response or exceptions instead."), "ERROR")
}

// bareExitParents are the expression contexts where tree-sitter-php parses a bare `die;`
// (or `exit;` inside an expression, e.g. `fopen($f) or exit;`) as a plain name.
var bareExitParents = map[string]bool{
	"expression_statement":     true,
	"binary_expression":        true,
	"conditional_expression":   true,
	"assignment_expression":    true,
	"parenthesized_expression": true,
	"return_statement":         true,
	"argument":                 true,
	"arrow_function":           true,
}

// handleBareExit reports a bare `die`/`exit` used as an expression, which tree-sitter-php parses as a name.
func (v *PHPVisitor) handleBareExit(n *sitter.Node) {
	parent := n.Parent()
	if parent == nil || !bareExitParents[parent.Kind()] {
		return
	}
	switch strings.ToLower(v.getContent(n)) {
	case "die", "exit":
		v.addExitFinding(n)
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

	rawName := v.getContent(nameNode)
	isFullyQualified := strings.HasPrefix(rawName, "\\")
	name := strings.ToLower(rawName)
	name = strings.TrimPrefix(name, "\\")

	// tree-sitter-php parses die(...) (and exit(...) as a PHP 8.4 function) as a call
	if name == "die" || name == "exit" {
		v.addExitFinding(n)
		return
	}

	rule, exists := v.processStateRule(n, name, isFullyQualified)
	if !exists {
		return
	}

	v.addFinding(n, symbol.RuleProcessStateMutation, rule.msg, v.hint(name, rule.hint), "WARNING")
}

// processStateRule returns the rule of a call to a function that changes the PHP process
// (ini_set(), date_default_timezone_set()). name is the lowercased function name without its
// leading backslash. An unqualified call shadowed by a function of the current namespace, or a
// getter call such as mb_internal_encoding() with no argument, does not match.
func (v *PHPVisitor) processStateRule(n *sitter.Node, name string, isFullyQualified bool) (dangerousFuncRule, bool) {
	rule, exists := dangerousFunctions[name]
	if !exists {
		return rule, false
	}

	// Unqualified call shadowed by use function or local function definition in the current namespace scope
	shadowed := v.getShadowedFunctions(n.StartByte())
	if !isFullyQualified && shadowed != nil && shadowed[name] {
		return rule, false
	}

	if rule.needsArgs && countArguments(n) == 0 {
		return rule, false
	}
	return rule, true
}

func (v *PHPVisitor) getShadowedFunctions(pos uint) map[string]bool {
	for _, sc := range v.namespaceScopes {
		if pos >= sc.startByte && pos < sc.endByte {
			return sc.shadowed
		}
	}
	return nil
}

func (v *PHPVisitor) collectNamespaceScopes(root *sitter.Node) {
	if root == nil {
		return
	}

	totalLen := uint(len(v.content))

	var nsNodes []*sitter.Node
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child.Kind() == "namespace_definition" {
			nsNodes = append(nsNodes, child)
		}
	}

	if len(nsNodes) == 0 {
		scope := namespaceScope{
			startByte: 0,
			endByte:   totalLen,
			shadowed:  make(map[string]bool),
		}
		v.scanScopeShadows(root, scope.shadowed)
		v.namespaceScopes = []namespaceScope{scope}
		return
	}

	isBracketed := false
	for _, ns := range nsNodes {
		for i := uint(0); i < ns.ChildCount(); i++ {
			if ns.Child(i).Kind() == "compound_statement" {
				isBracketed = true
				break
			}
		}
		if isBracketed {
			break
		}
	}

	if isBracketed {
		for _, ns := range nsNodes {
			scope := namespaceScope{
				startByte: ns.StartByte(),
				endByte:   ns.EndByte(),
				shadowed:  make(map[string]bool),
			}
			v.scanScopeShadows(ns, scope.shadowed)
			v.namespaceScopes = append(v.namespaceScopes, scope)
		}
		return
	}

	for i, ns := range nsNodes {
		start := ns.StartByte()
		if i == 0 {
			start = 0
		}
		end := totalLen
		if i+1 < len(nsNodes) {
			end = nsNodes[i+1].StartByte()
		}

		scope := namespaceScope{
			startByte: start,
			endByte:   end,
			shadowed:  make(map[string]bool),
		}
		for j := uint(0); j < root.ChildCount(); j++ {
			child := root.Child(j)
			if child.StartByte() >= start && child.StartByte() < end {
				v.scanScopeShadows(child, scope.shadowed)
			}
		}
		v.namespaceScopes = append(v.namespaceScopes, scope)
	}
}

func (v *PHPVisitor) scanScopeShadows(n *sitter.Node, shadowed map[string]bool) {
	if n == nil {
		return
	}

	var scan func(curr *sitter.Node, inClass bool)
	scan = func(curr *sitter.Node, inClass bool) {
		if curr == nil {
			return
		}
		kind := curr.Kind()
		switch kind {
		case "class_declaration", "trait_declaration", "interface_declaration", "enum_declaration", "anonymous_class":
			inClass = true
		case "function_definition":
			if !inClass {
				nameNode := curr.ChildByFieldName("name")
				if nameNode != nil {
					shadowed[strings.ToLower(v.getContent(nameNode))] = true
				}
			}
		case "namespace_use_declaration":
			v.extractFunctionUse(curr, shadowed)
		}

		for i := uint(0); i < curr.ChildCount(); i++ {
			scan(curr.Child(i), inClass)
		}
	}

	scan(n, false)
}

func (v *PHPVisitor) extractFunctionUse(n *sitter.Node, shadowed map[string]bool) {
	isDeclFunction := false
	for i := uint(0); i < n.ChildCount(); i++ {
		if n.Child(i).Kind() == "function" {
			isDeclFunction = true
			break
		}
	}

	var processClause func(clause *sitter.Node)
	processClause = func(clause *sitter.Node) {
		isClauseFunction := isDeclFunction
		for i := uint(0); i < clause.ChildCount(); i++ {
			if clause.Child(i).Kind() == "function" {
				isClauseFunction = true
				break
			}
		}
		if !isClauseFunction {
			return
		}

		aliasNode := clause.ChildByFieldName("alias")
		if aliasNode != nil {
			shadowed[strings.ToLower(v.getContent(aliasNode))] = true
			return
		}
		for i := uint(0); i < clause.ChildCount(); i++ {
			if clause.Child(i).Kind() == "as" {
				for j := i + 1; j < clause.ChildCount(); j++ {
					if clause.Child(j).Kind() == "name" {
						shadowed[strings.ToLower(v.getContent(clause.Child(j)))] = true
						return
					}
				}
			}
		}

		for i := uint(0); i < clause.ChildCount(); i++ {
			child := clause.Child(i)
			// use function array_map; imports the global function itself: nothing is shadowed.
			// In a group (use function App\Helper\{umask}), the name is under the group prefix.
			if child.Kind() == "name" {
				if clause.Parent() != nil && clause.Parent().Kind() == "namespace_use_group" {
					shadowed[strings.ToLower(v.getContent(child))] = true
				}
				return
			}
			if child.Kind() == "qualified_name" {
				parts := strings.Split(strings.TrimPrefix(v.getContent(child), "\\"), "\\")
				if len(parts) > 1 {
					shadowed[strings.ToLower(parts[len(parts)-1])] = true
				}
				return
			}
		}
	}

	var findClauses func(curr *sitter.Node)
	findClauses = func(curr *sitter.Node) {
		if curr.Kind() == "namespace_use_clause" {
			processClause(curr)
			return
		}
		for i := uint(0); i < curr.ChildCount(); i++ {
			findClauses(curr.Child(i))
		}
	}
	findClauses(n)
}

func countArguments(n *sitter.Node) int {
	argsNode := n.ChildByFieldName("arguments")
	if argsNode == nil {
		return 0
	}
	count := 0
	for i := uint(0); i < argsNode.ChildCount(); i++ {
		if argsNode.Child(i).Kind() == "argument" {
			count++
		}
	}
	return count
}

func (v *PHPVisitor) handleVariable(n *sitter.Node) {
	name := v.getContent(n)
	if isSuperglobal(name) && !isEnvRead(n, v.getContent) {
		v.addFinding(n, symbol.RuleSuperglobalsUsage, fmt.Sprintf("Usage of PHP Superglobal %s is discouraged in Worker mode.", name), v.hint("superglobal", "Use the Symfony Request object ($request->query, $request->request, etc.) instead."), "WARNING")
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
	if n == nil || v.skipsMutations() {
		return
	}
	if targets := writeTargets(n); targets != nil {
		// A destructuring, a reference or a foreach writes each of its targets
		for _, target := range targets {
			v.handleMutation(target)
		}
		return
	}

	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}

	switch n.Kind() {
	case "member_access_expression":
		// A transient (shared: false) service's instance state never outlives a request. Its
		// static properties belong to the class and do, so static writes are still checked.
		if !v.isTransientClass(fullName) {
			v.handleMemberAccess(n)
		}
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

// skipsMutations reports code whose writes are not reported: a readonly class, a constructor
// (it sets the initial state), code outside a method, or a class of a safe namespace.
func (v *PHPVisitor) skipsMutations() bool {
	if v.isReadonlyClass || v.curMethod == "__construct" || (v.curMethod == "" && v.curClass != "AnonymousClass" && v.curClass != "") {
		return true
	}
	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}
	return v.engine != nil && v.engine.IsSafeNamespace(fullName)
}

// writeTargets lists what a write node writes when it writes several things at once, or nil:
// the targets of a destructuring ([$this->a, $this->b] = ..., list(), keyed forms), of a reference
// ([&$this->a] = ...) and of a foreach (foreach ($rows as $this->current)).
func writeTargets(n *sitter.Node) []*sitter.Node {
	switch n.Kind() {
	case "list_literal":
		return destructuringTargets(n)
	case "foreach_statement":
		return foreachTargets(n)
	case "by_ref":
		if target := lastNamedChild(n); target != nil {
			return []*sitter.Node{target}
		}
		return []*sitter.Node{}
	}
	return nil
}

// isTransientClass reports whether the container dump registers the class as a non-shared service.
func (v *PHPVisitor) isTransientClass(fullName string) bool {
	return v.nonSharedServices[strings.TrimPrefix(fullName, "\\")] ||
		(v.engine != nil && v.engine.IsExplicitlyNonShared(fullName))
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

// handleAssignment handles variable assignments and tracks variables holding references to shared dependencies/services.
func (v *PHPVisitor) handleAssignment(n *sitter.Node) {
	if n == nil {
		return
	}

	left := n.ChildByFieldName("left")
	right := n.ChildByFieldName("right")

	// Writes that cannot carry state from one request to the next (#105)
	if v.isExemptWrite(n, left, right) {
		return
	}

	// Standard mutation detection on left side
	if left != nil {
		before := len(v.findings)
		v.handleMutation(left)
		v.tagKeyedCacheWrite(n, left, before)
	}

	// Dynamic local shared variable tracking
	if left != nil && right != nil {
		leftContent := v.getContent(left)
		rightContent := v.getContent(right)

		// If the left side is a simple variable (e.g. $entityManager)
		if strings.HasPrefix(leftContent, "$") && !strings.Contains(leftContent, "->") && !strings.Contains(leftContent, "[") {
			leftLower := strings.ToLower(leftContent)
			rightLower := strings.ToLower(rightContent)

			// Resolve and track local variable type from property fetch on $this or variable assignment
			if propName, ok := v.isPropertyFetchOnThis(right); ok {
				if typeName, exists := v.propertyTypes[propName]; exists {
					v.localVarTypes[leftContent] = typeName
				}
			} else if right.Kind() == "variable" || right.Kind() == "variable_name" {
				if typeName, exists := v.localVarTypes[rightContent]; exists {
					v.localVarTypes[leftContent] = typeName
				}
			}

			if v.isTransientOrEphemeral(leftContent, leftLower, rightLower, rightContent, right) {
				return
			}

			if v.isRightSideShared(right) {
				v.localSharedVars[leftContent] = true
			} else if v.wrapsSharedState(right, v.isRightSideShared) {
				v.localWrapperVars[leftContent] = true
			}
		}
	}
}

func isQueryBuilderOrExpression(leftLower, rightLower string) bool {
	return strings.Contains(rightLower, "querybuilder") ||
		strings.Contains(rightLower, "->expr(") ||
		strings.Contains(rightLower, "->orx(") ||
		strings.Contains(rightLower, "->andx(") ||
		strings.Contains(leftLower, "querybuilder") ||
		strings.Contains(leftLower, "expr")
}

func (v *PHPVisitor) isTransientOrEphemeral(leftContent, leftLower, rightLower, rightContent string, right *sitter.Node) bool {
	// Heuristic 1: Instantiations using the 'new' keyword are always fresh/transient objects, never shared
	if right.Kind() == "new_expression" || strings.HasPrefix(strings.TrimSpace(rightContent), "new ") {
		return true
	}

	// Heuristic 2: Ephemeral database QueryBuilders and Expressions are query-scoped, never shared across requests
	if isQueryBuilderOrExpression(leftLower, rightLower) {
		return true
	}

	// Heuristic 3: Check if it's a method call with semantic return-type tracking
	kind := right.Kind()
	if kind == "member_call_expression" || kind == "nullsafe_member_call_expression" {
		obj := right.ChildByFieldName("object")
		nameNode := right.ChildByFieldName("name")
		if obj != nil && nameNode != nil && v.engine != nil {
			receiverClass := v.resolveReceiverType(obj)
			methodName := v.getContent(nameNode)
			if receiverClass != "" && methodName != "" {
				retType := v.engine.GetMethodReturnType(receiverClass, methodName)
				if retType != "" {
					// Save type in local variable tracking
					v.localVarTypes[leftContent] = retType

					// If the return type is not a shared service, it is transient!
					if !v.engine.IsSharedService(retType) {
						return true
					}
				}
			}
		}
	}

	// Heuristic 4: Known factory, builder and safe transient infrastructure methods (fallback/legacy)
	return strings.Contains(rightLower, "->find") ||
		strings.Contains(rightLower, "->create") ||
		strings.Contains(rightLower, "->build") ||
		strings.Contains(rightLower, "->getitem")
}

func (v *PHPVisitor) resolveReceiverType(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	kind := n.Kind()

	// Case 1: property fetch on $this, e.g. $this->tracer
	if propName, ok := v.isPropertyFetchOnThis(n); ok {
		if typeName, exists := v.propertyTypes[propName]; exists {
			return v.resolveFQCN(typeName)
		}
	}

	// Case 2: variable, e.g. $tracer or $this
	if kind == "variable" || kind == "variable_name" {
		content := v.getContent(n)
		if content == "$this" {
			fullName := v.curClass
			if v.namespace != "" {
				fullName = v.namespace + "\\" + v.curClass
			}
			return fullName
		}
		if typeName, exists := v.localVarTypes[content]; exists {
			return v.resolveFQCN(typeName)
		}
	}

	// Case 3: member_call_expression / nullsafe_member_call_expression
	if kind == "member_call_expression" || kind == "nullsafe_member_call_expression" {
		obj := n.ChildByFieldName("object")
		nameNode := n.ChildByFieldName("name")
		if obj != nil && nameNode != nil {
			receiverClass := v.resolveReceiverType(obj)
			methodName := v.getContent(nameNode)
			if receiverClass != "" && methodName != "" && v.engine != nil {
				return v.engine.GetMethodReturnType(receiverClass, methodName)
			}
		}
	}

	return ""
}

// isRightSideShared reports whether an assigned value is the shared service's own state: $this,
// an object reached from it or from a static property, a self::/static:: call, or a local variable
// already tracked as shared, a call given any of these (current($this->queues) may return one of
// them), or an array holding one. A value built by new, clone or a known builder is a fresh
// value, even when $this appears in its arguments (collect([$this->a, $this->b])): it is
// tracked as a wrapper instead.
func (v *PHPVisitor) isRightSideShared(right *sitter.Node) bool {
	if right == nil {
		return false
	}
	switch right.Kind() {
	case "parenthesized_expression", "conditional_expression", "binary_expression",
		"array_creation_expression", "array_element_initializer", "pair":
		// [$this->queue] is a new array, but the queue in it is still shared
		for i := uint(0); i < right.ChildCount(); i++ {
			if v.isRightSideShared(right.Child(i)) {
				return true
			}
		}
		return false
	}
	if v.derivesFromOwnState(right) {
		return true
	}
	// Helper::get($this->requestStack) may hand back what it was given: a static call taking
	// shared state stays shared, unlike a function call (collect(), array_map()) or new
	if right.Kind() == "scoped_call_expression" && v.passesSharedState(right) {
		return true
	}
	if v.returnsArgument(right, v.isRightSideShared) {
		return true
	}
	root := rootVariable(v, right)
	return root != "" && v.localSharedVars[root]
}

func (v *PHPVisitor) handleScopedAccess(n *sitter.Node) {
	scope := n.ChildByFieldName("scope")
	nameNode := n.ChildByFieldName("name")
	if scope == nil || nameNode == nil {
		return
	}
	prop := v.getContent(nameNode)

	switch s := v.getContent(scope); strings.ToLower(s) {
	case "self", "static":
		v.logMutation(n, prop, true)
	case "parent":
		// parent::$x is the current class's static unless the class redeclares it
		if v.declaredProps[strings.TrimPrefix(prop, "$")] {
			v.logForeignStaticMutation(n, "parent", prop, false)
			return
		}
		v.logMutation(n, prop, true)
	default:
		// ClassName::$x: static state persists across requests whichever class writes it
		target := s
		k := scope.Kind()
		dynamic := k != "name" && k != "qualified_name"
		if !dynamic {
			target = v.resolveFQCN(s)
		} // else a dynamic scope ($class::$x): keep the expression as written
		current := v.curClass
		if v.namespace != "" {
			current = v.namespace + "\\" + v.curClass
		}
		// PHP class names are case-insensitive
		if v.curClass != "" && strings.EqualFold(target, current) {
			v.logMutation(n, prop, true)
			return
		}
		v.logForeignStaticMutation(n, target, prop, dynamic)
	}
}

// logForeignStaticMutation tracks a write to another class's static property. Like a write to
// the class's own statics, it counts as a reset inside reset() and must be reset in resettable classes.
func (v *PHPVisitor) logForeignStaticMutation(n *sitter.Node, target, prop string, dynamic bool) {
	key := "static::" + target + "::" + prop
	if v.curMethod == "reset" {
		// A cleanup counts even on an excluded line; $class::$x may target another class
		if !dynamic {
			v.resetted[key] = true
		}
		return
	}
	// Explicit exclusions apply before the write is tracked for the reset check
	if v.isWorkerSafeClass || v.isWorkerSafeMethod || v.isIgnoredLine(n) {
		return
	}
	switch {
	case v.isReset && !dynamic:
		v.recordMutation(key, mutationInfo{
			method:     v.curMethod,
			line:       int(n.StartPosition().Row) + 1,
			code:       v.lines[n.StartPosition().Row],
			snippet:    v.getContent(n),
			astDetails: n.ToSexp(),
		})
	default:
		msg := fmt.Sprintf("Mutation of static state '%s::%s' in %s::%s()", target, prop, v.curClass, v.curMethod)
		if v.curClass == "" {
			msg = fmt.Sprintf("Mutation of static state '%s::%s'", target, prop)
		}
		v.addFinding(n, symbol.RuleStateMutation, msg, "Static properties persist across requests in Worker mode, whichever class writes them.", "ERROR")
	}
}

func (v *PHPVisitor) logMutation(n *sitter.Node, prop string, static bool) {
	key := prop
	if static {
		key = "static::" + prop
	}

	// Skip if property is readonly, WorkerSafe, or cleaned up in a finally block
	if !static && (v.readonlyProps[prop] || v.workerSafeProps[prop] || v.finallyCleaned[prop]) {
		return
	}
	if static && v.workerSafeProps[prop] {
		return
	}

	switch {
	case v.curMethod == "reset":
		v.resetted[key] = true
	case v.isReset:
		// Explicit exclusions apply before the write is tracked for the reset check
		if v.isWorkerSafeClass || v.isWorkerSafeMethod || v.isIgnoredLine(n) {
			return
		}
		v.recordMutation(key, mutationInfo{
			method:     v.curMethod,
			line:       int(n.StartPosition().Row) + 1,
			code:       v.lines[n.StartPosition().Row],
			snippet:    v.getContent(n),
			astDetails: n.ToSexp(),
		})
	case v.curClass != "" || static:
		msg := fmt.Sprintf("Mutation of state '%s' in %s::%s()", key, v.curClass, v.curMethod)
		v.addFinding(n, symbol.RuleStateMutation, msg, "State mutations persist across requests in Worker mode.", "ERROR")
	}
}

// recordMutation keeps the last write of a property for the report, and every method that
// mutates it: the finding is reachable as soon as one of them is called.
func (v *PHPVisitor) recordMutation(prop string, info mutationInfo) {
	methods := v.mutated[prop].methods
	if info.method != "" && !slices.Contains(methods, info.method) {
		methods = append(methods, info.method)
	}
	info.methods = methods
	v.mutated[prop] = info
}

func (v *PHPVisitor) performResetCheck() {
	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}

	for prop, info := range v.mutated {
		if v.workerSafeProps[prop] {
			continue
		}
		// A static write always targets real static state, even one inherited or from a trait,
		// so only instance properties must be declared in this class to be checked
		if !strings.HasPrefix(prop, "static::") && !v.declaredProps[prop] {
			continue
		}
		if !v.resetted[prop] {
			v.findings = append(v.findings, symbol.Finding{
				Rule:          symbol.RuleIncompleteReset,
				Message:       fmt.Sprintf("Property '%s' of %s is mutated but not reset in reset().", prop, v.curClass),
				Severity:      "WARNING",
				Line:          info.line,
				Code:          info.code,
				Snippet:       info.snippet,
				ASTDetails:    info.astDetails,
				Dependencies:  v.dependencies,
				Remediation:   fmt.Sprintf("Add '$this->%s = ...' in the reset() method.", prop),
				ContextClass:  fullName,
				ContextMethod: info.method,
				AlsoMethods:   otherMethods(info.methods, info.method),
				Category:      symbol.CategoryNotReset,
				Property:      prop,
			})
		}
	}
}

func otherMethods(methods []string, method string) []string {
	var others []string
	for _, m := range methods {
		if m != method {
			others = append(others, m)
		}
	}
	return others
}

// isIgnoredLine reports whether the node's line or the line above it carries @igor-ignore.
func (v *PHPVisitor) isIgnoredLine(n *sitter.Node) bool {
	row := int(n.StartPosition().Row)
	if strings.Contains(v.lines[row], "@igor-ignore") {
		return true
	}
	return row > 0 && strings.Contains(v.lines[row-1], "@igor-ignore")
}

// addFinding reports a finding of the given rule (a symbol.Rule* identifier) on the node.
func (v *PHPVisitor) addFinding(n *sitter.Node, rule, msg, hint, severity string) {
	if v.isWorkerSafeClass || v.isWorkerSafeMethod {
		return
	}

	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}
	if v.engine != nil && v.engine.IsSafeNamespace(fullName) {
		return
	}

	if v.isIgnoredLine(n) {
		return
	}

	row := int(n.StartPosition().Row)
	v.findings = append(v.findings, symbol.Finding{
		Rule:          rule,
		Message:       msg,
		Line:          row + 1,
		Code:          v.lines[row],
		Snippet:       v.getContent(n),
		ASTDetails:    n.ToSexp(),
		Dependencies:  v.dependencies,
		Remediation:   hint,
		Severity:      severity,
		ContextClass:  fullName,
		ContextMethod: v.curMethod,
	})
}

func (v *PHPVisitor) getContent(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return string(v.content[n.StartByte():n.EndByte()])
}

// hasAttribute checks if the given node has an attribute ending with the target name (e.g. "WorkerSafe").
func (v *PHPVisitor) hasAttribute(n *sitter.Node, target string) bool {
	if n == nil {
		return false
	}
	attributesNode := n.ChildByFieldName("attributes")
	if attributesNode == nil {
		for i := 0; i < int(n.ChildCount()); i++ {
			c := n.Child(uint(i))
			if c.Kind() == "attribute_list" || c.Kind() == "attributes" {
				attributesNode = c
				break
			}
		}
	}
	if attributesNode == nil {
		return false
	}

	for i := uint(0); i < attributesNode.ChildCount(); i++ {
		group := attributesNode.Child(i)
		if group.Kind() == "attribute_group" {
			for j := uint(0); j < group.ChildCount(); j++ {
				attr := group.Child(j)
				if attr.Kind() == "attribute" {
					for k := uint(0); k < attr.ChildCount(); k++ {
						nameNode := attr.Child(k)
						if nameNode.Kind() == "name" || nameNode.Kind() == "qualified_name" || nameNode.Kind() == "fully_qualified_name" {
							nameContent := v.getContent(nameNode)
							if nameContent == target || strings.HasSuffix(nameContent, "\\"+target) {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

// handleMethodCall intercepts and analyzes method calls (e.g., $this->dispatcher->addListener() or $this->googleTagManager->addPush()).
func (v *PHPVisitor) handleMethodCall(n *sitter.Node) {
	if n == nil {
		return
	}

	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}

	// 1. Retrieve the calling receiver object and the method name
	obj := n.ChildByFieldName("object")
	if obj == nil {
		return
	}

	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	methodName := v.getContent(nameNode)

	if v.engine != nil {
		if receiverClass := v.resolveReceiverType(obj); receiverClass != "" {
			v.engine.RecordMethodCall(fullName, v.curMethod, receiverClass, methodName)
		}
	}

	// If the service is explicitly transient (non-shared), mutations are accepted
	if v.nonSharedServices[strings.TrimPrefix(fullName, "\\")] {
		return
	}
	if v.engine != nil && (v.engine.IsExplicitlyNonShared(fullName) || v.engine.IsSafeNamespace(fullName)) {
		return
	}

	// 2. Check if the object/chain starts with a property of the current class ($this->propertyName)
	propName, isProp := v.resolveRootProperty(obj)
	if !isProp {
		return
	}

	_, isDirectProp := v.isPropertyFetchOnThis(obj)

	// Run Rule 1: DetectSingletonMutationRule
	v.detectSingletonMutation(n, propName, methodName, isDirectProp)

	// Run Rule 2: DetectClosureStateLeakRule
	v.detectClosureStateLeak(n)
}

// dependencyMutationMessage names the changed object: a local holding shared state, or an
// injected dependency.
func (v *PHPVisitor) dependencyMutationMessage(propName string) string {
	if v.isTrackedLocal("$"+propName, true) {
		return fmt.Sprintf("Mutation detected on a local reference to a shared service ($%s). Risk of State Leak in a worker.", propName)
	}
	return fmt.Sprintf("Mutation detected on an injected dependency ($this->%s). Risk of State Leak in a worker.", propName)
}

// detectSingletonMutation flags runtime mutations on injected dependencies unless they are resettable.
func (v *PHPVisitor) detectSingletonMutation(n *sitter.Node, propName, methodName string, isDirectProp bool) {
	if !hasMutationPrefix(methodName) {
		return
	}

	// If the current class implements ResetInterface, we treat this as a standard property mutation.
	// It is allowed as long as the property itself is cleared/reset in the reset() method.
	if v.isReset {
		if isDirectProp {
			v.recordMutation(propName, mutationInfo{
				method:     v.curMethod,
				line:       int(n.StartPosition().Row) + 1,
				code:       v.lines[n.StartPosition().Row],
				snippet:    v.getContent(n),
				astDetails: n.ToSexp(),
			})
		}
		return
	}

	// Otherwise, check if the injected type implements ResetInterface in the Symfony container
	isResettable := false
	typeName := ""
	hasType := false

	if t, ok := v.propertyTypes[propName]; ok {
		typeName = t
		hasType = true
	} else if t, ok := v.localVarTypes["$"+propName]; ok {
		typeName = t
		hasType = true
	}

	if hasType {
		fqcn := v.resolveFQCN(typeName)
		if v.engine != nil {
			if v.engine.IsSafeNamespace(fqcn) {
				isResettable = true
			} else if v.engine.IsResettable(fqcn) {
				if isDoctrineManager(fqcn) {
					obj := n.ChildByFieldName("object")
					if obj != nil && v.isDirectReference(obj) && isUnitOfWorkLifecycleMethod(methodName) {
						isResettable = true
					} else {
						isResettable = false
					}
				} else {
					isResettable = true
				}
			}
		}
	}

	if !isResettable {
		msg := v.dependencyMutationMessage(propName)
		before := len(v.findings)
		v.addFinding(n, symbol.RuleSingletonMutation, msg, v.hint("dependency_mutation", "Avoid modifying injected dependencies at runtime, or use a ResetInterface."), "ERROR")
		// The method name is only a guess: let the auditor drop the finding once it has read that method
		if len(v.findings) > before && isDirectProp && !v.localSharedVars["$"+propName] {
			if class := v.propertyClass(propName); class != "" {
				shared, unknown := v.sharedArgPositions(n)
				v.findings[before].Callee = &symbol.CallRef{Class: class, Method: methodName, SharedArgs: shared, UnknownArgs: unknown}
			}
		}
	}
}

// detectClosureStateLeak flags anonymous functions capturing local scope via use() passed as arguments to singleton dependencies.
// Whether the called method keeps the closure or only calls it is not known here: the finding
// names the call, and the auditor drops it once it has read that method.
func (v *PHPVisitor) detectClosureStateLeak(n *sitter.Node) {
	argsNode := n.ChildByFieldName("arguments")
	if argsNode == nil {
		return
	}

	ref := symbol.ClosureArgRef{Class: v.resolveReceiverType(n.ChildByFieldName("object")), Positional: true}
	if nameNode := n.ChildByFieldName("name"); nameNode != nil && nameNode.Kind() == "name" {
		ref.Method = v.getContent(nameNode)
	}

	var findClosures func(*sitter.Node)
	findClosures = func(node *sitter.Node) {
		if node == nil {
			return
		}
		// If the node is an anonymous function (closure)
		if node.Kind() == "anonymous_function" {
			// And it has a "use" capture clause (use ($var))
			if hasUseClause(node) {
				msg := "Potential Memory Leak: Injection of a closure capturing local state into a shared service."
				before := len(v.findings)
				v.addFinding(node, symbol.RuleClosureStateLeak, msg, "Avoid injecting closures that capture local state via use() into shared services.", "ERROR")
				if len(v.findings) > before {
					arg := ref
					v.findings[before].ClosureArg = &arg
				}
			}
		}
		// Recursive walk of argument node children (e.g., in a nested array)
		for i := uint(0); i < node.ChildCount(); i++ {
			findClosures(node.Child(i))
		}
	}
	for i := uint(0); i < argsNode.NamedChildCount(); i++ {
		arg := argsNode.NamedChild(i)
		value := lastNamedChild(arg)
		if arg.ChildByFieldName("name") != nil || arg.Kind() == "variadic_unpacking" || (value != nil && value.Kind() == "variadic_unpacking") {
			// A named or spread argument: this position and the next ones cannot be trusted
			ref.Positional = false
		}
		ref.Pos = int(i)
		findClosures(arg)
	}
}

func isDoctrineManager(className string) bool {
	normalized := strings.ReplaceAll(className, "/", "\\")
	normalized = strings.TrimPrefix(normalized, "\\")
	lower := strings.ToLower(normalized)
	return strings.Contains(lower, "doctrine\\orm\\entitymanager") ||
		strings.Contains(lower, "doctrine\\persistence\\objectmanager") ||
		strings.Contains(lower, "doctrine\\odm\\mongodb\\documentmanager")
}

func isUnitOfWorkLifecycleMethod(methodName string) bool {
	switch methodName {
	case "persist", "remove", "flush", "clear", "refresh", "detach", "merge":
		return true
	}
	return false
}

func (v *PHPVisitor) isDirectReference(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	if _, ok := v.isPropertyFetchOnThis(n); ok {
		return true
	}
	kind := n.Kind()
	if kind == "variable" || kind == "variable_name" {
		content := v.getContent(n)
		if strings.HasPrefix(content, "$") && v.localSharedVars[content] {
			return true
		}
	}
	return false
}

// isPropertyFetchOnThis checks if a node is a property of the current class via $this (e.g., $this->propertyName)
func (v *PHPVisitor) isPropertyFetchOnThis(n *sitter.Node) (string, bool) {
	if n == nil {
		return "", false
	}
	if n.Kind() != "member_access_expression" {
		return "", false
	}
	obj := n.ChildByFieldName("object")
	if obj == nil {
		return "", false
	}
	objContent := v.getContent(obj)
	// Check if the access is made on the $this object
	if !strings.Contains(objContent, "$this") {
		return "", false
	}
	nameNode := n.ChildByFieldName("name")
	if nameNode == nil {
		return "", false
	}
	return v.getContent(nameNode), true
}

// resolveRootProperty traverses down a chain of member calls, member accesses, and subscripts
// to find if the root is a property fetch on $this (or a tracked local shared variable).
func (v *PHPVisitor) resolveRootProperty(n *sitter.Node) (string, bool) {
	curr := n
	for curr != nil {
		kind := curr.Kind()
		switch kind {
		case "member_call_expression", "nullsafe_member_call_expression":
			if v.isChainBrokenByMemberCall(curr) {
				return "", false
			}
			curr = curr.ChildByFieldName("object")
		case "member_access_expression", "nullsafe_member_access_expression":
			obj := curr.ChildByFieldName("object")
			if obj != nil {
				// If the object's kind is NOT member_access_expression or member_call_expression,
				// and it contains $this, then curr is the root property access on $this.
				objKind := obj.Kind()
				if objKind != "member_access_expression" && objKind != "nullsafe_member_access_expression" &&
					objKind != "member_call_expression" && objKind != "nullsafe_member_call_expression" {
					objContent := v.getContent(obj)
					if strings.Contains(objContent, "$this") {
						nameNode := curr.ChildByFieldName("name")
						if nameNode != nil {
							return v.getContent(nameNode), true
						}
						return "", false
					}
				}
			}
			curr = obj
		case "subscript_expression":
			if curr.ChildCount() > 0 {
				curr = curr.Child(0)
			} else {
				curr = nil
			}
		default:
			// If we reached a variable node, check if it's a tracked local shared variable, or a
			// wrapper reached through ($copy->queue->push(), not $copy->push())
			content := v.getContent(curr)
			if v.isTrackedLocal(content, curr != n) {
				return strings.TrimPrefix(content, "$"), true
			}
			curr = nil
		}
	}
	// Fallback check on final node
	if curr != nil {
		content := v.getContent(curr)
		if strings.HasPrefix(content, "$") && v.localSharedVars[content] {
			return strings.TrimPrefix(content, "$"), true
		}
	}
	return "", false
}

// isTrackedLocal reports a local variable holding shared state, or a wrapper of it ($copy = clone
// $this->holder) when the change goes through it (reached) rather than to it.
func (v *PHPVisitor) isTrackedLocal(content string, reached bool) bool {
	if !strings.HasPrefix(content, "$") {
		return false
	}
	return v.localSharedVars[content] || (reached && v.localWrapperVars[content])
}

// isChainBrokenByMemberCall checks if a member call expression breaks the taint chain.
func (v *PHPVisitor) isChainBrokenByMemberCall(curr *sitter.Node) bool {
	nameNode := curr.ChildByFieldName("name")
	if nameNode == nil {
		return false
	}
	methodName := v.getContent(nameNode)
	obj := curr.ChildByFieldName("object")

	// Try semantic return-type tracking first
	if obj != nil && v.engine != nil {
		receiverClass := v.resolveReceiverType(obj)
		if receiverClass != "" {
			retType := v.engine.GetMethodReturnType(receiverClass, methodName)
			if retType != "" {
				// If the return type is NOT a shared service, it breaks the taint chain!
				if !v.engine.IsSharedService(retType) {
					return true
				}
			}
		}
	}

	// Fallback to method-name based taint breakers
	return isTaintBreakerMethod(methodName)
}

// isTaintBreakerMethod checks if the method is a factory, builder, or query-scoped method that breaks the taint chain
func isTaintBreakerMethod(methodName string) bool {
	m := strings.ToLower(methodName)
	return strings.HasPrefix(m, "find") ||
		strings.HasPrefix(m, "create") ||
		strings.HasPrefix(m, "build") ||
		m == "getitem" ||
		m == "getitems" ||
		m == "expr"
}

// hasMutationPrefix checks if the method name starts with a standard mutation prefix
func hasMutationPrefix(methodName string) bool {
	prefixes := []string{"add", "set", "push", "register", "append", "disable", "enable", "clear", "remove"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(methodName, prefix) {
			// Ensure the prefix is properly bounded (e.g., camelCase/PascalCase) to avoid false positives like "settle" or "address"
			if len(methodName) == len(prefix) {
				return true
			}
			nextChar := methodName[len(prefix)]
			if nextChar >= 'A' && nextChar <= 'Z' {
				return true
			}
			if nextChar == '_' {
				return true
			}
		}
	}
	return false
}

// hasUseClause checks if an anonymous function has a "use (...)" capture clause
func hasUseClause(node *sitter.Node) bool {
	for i := uint(0); i < node.ChildCount(); i++ {
		if node.Child(i).Kind() == "anonymous_function_use_clause" {
			return true
		}
	}
	return false
}

// scanPropertyTypes recursively extracts type-hints of class properties and promoted constructor parameters.
func (v *PHPVisitor) scanPropertyTypes(classNode *sitter.Node) {
	body := v.getClassBody(classNode)
	if body == nil {
		return
	}

	for i := uint(0); i < body.ChildCount(); i++ {
		member := body.Child(i)
		if member.Kind() == "property_declaration" {
			v.handlePropertyDeclaration(member)
		}
		if member.Kind() == "method_declaration" {
			v.handleConstructorDeclaration(member)
		}
	}
}

func (v *PHPVisitor) handlePropertyDeclaration(member *sitter.Node) {
	typeNode := member.ChildByFieldName("type")
	var typeStr string
	if typeNode != nil {
		typeStr = v.getContent(typeNode)
	}
	for j := uint(0); j < member.ChildCount(); j++ {
		child := member.Child(j)
		if child.Kind() == "property_element" {
			nameNode := child.ChildByFieldName("name")
			if nameNode != nil {
				propName := strings.TrimPrefix(v.getContent(nameNode), "$")
				if typeStr != "" {
					v.propertyTypes[propName] = typeStr
				}
				v.declaredProps[propName] = true
			}
		}
	}
}

func (v *PHPVisitor) handleConstructorDeclaration(member *sitter.Node) {
	nameNode := member.ChildByFieldName("name")
	if nameNode == nil || strings.ToLower(v.getContent(nameNode)) != "__construct" {
		return
	}
	params := member.ChildByFieldName("parameters")
	if params == nil {
		for i := 0; i < int(member.ChildCount()); i++ {
			child := member.Child(uint(i))
			if child.Kind() == "formal_parameters" {
				params = child
				break
			}
		}
	}
	if params == nil {
		return
	}
	for j := uint(0); j < params.ChildCount(); j++ {
		param := params.Child(j)
		if param.Kind() == "parameter_declaration" || param.Kind() == "property_promotion_parameter" || param.Kind() == "simple_parameter" {
			typeNode := param.ChildByFieldName("type")
			var typeStr string
			if typeNode != nil {
				typeStr = v.getContent(typeNode)
			}
			for k := uint(0); k < param.ChildCount(); k++ {
				child := param.Child(k)
				if child.Kind() == "variable_name" {
					propName := strings.TrimPrefix(v.getContent(child), "$")
					if typeStr != "" {
						v.propertyTypes[propName] = typeStr
					}
					if param.Kind() == "property_promotion_parameter" {
						v.declaredProps[propName] = true
					}
					break
				}
			}
		}
	}
}

// resolveFQCN resolves the Fully Qualified Class Name (FQCN) of a type-hint by scanning the "use" imports of the file.
func (v *PHPVisitor) resolveFQCN(typeName string) string {
	if strings.HasPrefix(typeName, "\\") {
		return strings.TrimPrefix(typeName, "\\")
	}

	// 1. Search among the "use" imports of the current file
	if fqcn, ok := v.lookupImport(typeName); ok {
		return fqcn
	}

	// 1b. Qualified name (Alias\Foo): an import can only match its first segment
	if first, rest, ok := strings.Cut(typeName, "\\"); ok {
		if fqcn, ok := v.lookupImport(first); ok {
			return fqcn + "\\" + rest
		}
	}

	// 2. Fallback to the current namespace
	if v.namespace != "" {
		return v.namespace + "\\" + typeName
	}
	return typeName
}

// lookupGroupedImport resolves a name through the members of a grouped import.
func lookupGroupedImport(prefix, group, typeName string) (string, bool) {
	for _, member := range strings.Split(group, ",") {
		member = strings.TrimSpace(member)
		name, alias, aliased := strings.Cut(member, " as ")
		name = strings.TrimSpace(name)
		if !aliased {
			alias = name[strings.LastIndex(name, "\\")+1:]
		}
		if name != "" && strings.TrimSpace(alias) == typeName {
			return prefix + name, true
		}
	}
	return "", false
}

// lookupImport resolves a class name or alias through the "use" imports of the current file.
func (v *PHPVisitor) lookupImport(typeName string) (string, bool) {
	for _, line := range v.lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "use ") && strings.HasSuffix(line, ";") {
			importPart := strings.TrimSuffix(strings.TrimPrefix(line, "use "), ";")
			importPart = strings.TrimSpace(importPart)
			// Function and constant imports never name a class
			if strings.HasPrefix(importPart, "function ") || strings.HasPrefix(importPart, "const ") {
				continue
			}
			// Grouped import on one line: use Vendor\Package\{Foo, Bar as Baz};
			if prefix, group, ok := strings.Cut(importPart, "{"); ok {
				if fqcn, found := lookupGroupedImport(strings.TrimSpace(prefix), strings.TrimSuffix(strings.TrimSpace(group), "}"), typeName); found {
					return fqcn, true
				}
				continue
			}
			if strings.Contains(importPart, " as ") {
				parts := strings.Split(importPart, " as ")
				if len(parts) == 2 {
					fqcn := strings.TrimSpace(parts[0])
					alias := strings.TrimSpace(parts[1])
					if alias == typeName {
						return fqcn, true
					}
				}
			} else {
				parts := strings.Split(importPart, "\\")
				lastPart := parts[len(parts)-1]
				if lastPart == typeName {
					return importPart, true
				}
			}
		}
	}
	return "", false
}

// getFinallyCleanedProperties scans a finally_clause AST node to extract properties of $this that are cleaned up.
func (v *PHPVisitor) getFinallyCleanedProperties(finallyNode *sitter.Node) map[string]bool {
	cleanups := make(map[string]bool)
	if finallyNode == nil {
		return cleanups
	}

	var walkFinally func(*sitter.Node)
	walkFinally = func(node *sitter.Node) {
		if node == nil {
			return
		}

		switch node.Kind() {
		case "function_call_expression":
			v.checkFinallyFunctionCall(node, cleanups)
		case "assignment_expression":
			v.checkFinallyAssignment(node, cleanups)
		case "unset_statement":
			v.checkFinallyUnset(node, cleanups)
		}

		for i := uint(0); i < node.ChildCount(); i++ {
			walkFinally(node.Child(i))
		}
	}

	walkFinally(finallyNode)
	return cleanups
}

// checkFinallyFunctionCall detects cleanup function calls like array_pop($this->propertyName) or array_shift($this->propertyName).
func (v *PHPVisitor) checkFinallyFunctionCall(node *sitter.Node, cleanups map[string]bool) {
	fnNameNode := node.ChildByFieldName("function")
	if fnNameNode == nil {
		fnNameNode = node.ChildByFieldName("name")
	}
	if fnNameNode == nil {
		return
	}
	fnName := strings.ToLower(v.getContent(fnNameNode))
	if fnName != "array_pop" && fnName != "array_shift" {
		return
	}
	argsNode := node.ChildByFieldName("arguments")
	if argsNode == nil {
		return
	}
	for i := uint(0); i < argsNode.ChildCount(); i++ {
		arg := argsNode.Child(i)
		if arg.Kind() == "argument" && arg.NamedChildCount() > 0 {
			expr := arg.NamedChild(0)
			if expr != nil && expr.Kind() == "member_access_expression" {
				prop, isProp := v.isPropertyFetchOnThis(expr)
				if isProp {
					cleanups[prop] = true
				}
			}
		}
	}
}

// checkFinallyAssignment detects cleanup assignments like $this->propertyName = null or $this->propertyName = [].
func (v *PHPVisitor) checkFinallyAssignment(node *sitter.Node, cleanups map[string]bool) {
	left := node.ChildByFieldName("left")
	if left != nil && left.Kind() == "member_access_expression" {
		prop, isProp := v.isPropertyFetchOnThis(left)
		if isProp {
			cleanups[prop] = true
		}
	}
}

// checkFinallyUnset detects cleanup unset statements like unset($this->propertyName).
func (v *PHPVisitor) checkFinallyUnset(node *sitter.Node, cleanups map[string]bool) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "member_access_expression" {
			prop, isProp := v.isPropertyFetchOnThis(child)
			if isProp {
				cleanups[prop] = true
			}
		}
	}
}

// preScanFinallyCleanups recursively scans a method or function body to extract properties cleaned up in finally blocks.
func (v *PHPVisitor) preScanFinallyCleanups(n *sitter.Node) {
	body := n.ChildByFieldName("body")
	if body == nil {
		return
	}
	var preScanFinally func(*sitter.Node)
	preScanFinally = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if node.Kind() == "try_statement" {
			for i := uint(0); i < node.ChildCount(); i++ {
				child := node.Child(i)
				if child.Kind() == "finally_clause" {
					cleanups := v.getFinallyCleanedProperties(child)
					for prop := range cleanups {
						v.finallyCleaned[prop] = true
					}
				}
			}
		}
		for i := uint(0); i < node.ChildCount(); i++ {
			preScanFinally(node.Child(i))
		}
	}
	preScanFinally(body)
}
