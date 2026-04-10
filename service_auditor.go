package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// ServiceAuditor audits Symfony services for statelessness.
type ServiceAuditor struct {
	container      *SymfonyContainer
	AuditedClasses map[string]bool
	classToFile    map[string]string
	mu             sync.Mutex
	Config         Config
}

// NewServiceAuditor creates a new instance of ServiceAuditor.
func NewServiceAuditor(cfg Config) *ServiceAuditor {
	return &ServiceAuditor{
		AuditedClasses: make(map[string]bool),
		classToFile:    make(map[string]string),
		Config:         cfg,
	}
}

// LoadSymfonyContainer fetches definitions and LOCATES files via PHP Reflection in PROD mode.
func (a *ServiceAuditor) LoadSymfonyContainer(root string) error {
	consolePath := filepath.Join(root, "bin", "console")
	
	fmt.Println("🚀 Querying Symfony container in PROD mode...")
	cmd := exec.Command("php", consolePath, "debug:container", "--format=json", "--show-hidden", "--env=prod", "--no-debug")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute debug:container: %v", err)
	}

	strOutput := string(output)
	start := strings.Index(strOutput, "{")
	end := strings.LastIndex(strOutput, "}")
	if start == -1 || end == -1 || end < start {
		return fmt.Errorf("could not find a valid JSON object in Symfony output")
	}
	jsonPart := strOutput[start : end+1]

	var container SymfonyContainer
	if err := json.Unmarshal([]byte(jsonPart), &container); err != nil {
		return fmt.Errorf("failed to parse Symfony container JSON: %v", err)
	}
	a.container = &container

	fmt.Println("🔍 Locating service files via PHP Reflection (PROD vendors)...")
	cwd, _ := filepath.Abs(".")
	helperPath := filepath.Join(cwd, "find_class_files.php")

	reflectCmd := exec.Command("php", helperPath, root)
	reflectCmd.Stdin = bytes.NewReader([]byte(jsonPart))
	
	reflectOutput, err := reflectCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to locate files via reflection: %v", err)
	}

	var mapping map[string]string
	if err := json.Unmarshal(reflectOutput, &mapping); err != nil {
		return fmt.Errorf("failed to parse reflection mapping: %v", err)
	}
	
	a.classToFile = mapping
	fmt.Printf("✅ Located %d production service files for auditing.\n", len(mapping))

	return nil
}

// Audit analyzes a PHP file for state mutations in classes.
func (a *ServiceAuditor) Audit(path string) ([]Finding, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	p := sitter.NewParser()
	lang := sitter.NewLanguage(php.LanguagePHP())
	_ = p.SetLanguage(lang)

	tree := p.Parse(content, nil)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse %s", path)
	}
	defer tree.Close()

	v := &PHPVisitor{
		content:   content,
		lines:     strings.Split(string(content), "\n"),
		mutated:   make(map[string]mutationInfo),
		resetted:  make(map[string]bool),
		container: a.container,
		auditor:   a,
	}
	v.walk(tree.RootNode())

	return v.findings, nil
}

// ExtractFQCN returns the Full Qualified Class Name from a PHP file.
func (a *ServiceAuditor) ExtractFQCN(path string) (string, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}

	p := sitter.NewParser()
	lang := sitter.NewLanguage(php.LanguagePHP())
	_ = p.SetLanguage(lang)

	tree := p.Parse(content, nil)
	if tree == nil {
		return "", fmt.Errorf("failed to parse %s", path)
	}
	defer tree.Close()

	var namespace string
	var className string

	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil { return }
		
		switch n.Kind() {
		case "namespace_definition":
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				namespace = string(content[nameNode.StartByte():nameNode.EndByte()])
			}
		case "class_declaration", "trait_declaration":
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				className = string(content[nameNode.StartByte():nameNode.EndByte()])
			}
		}
		
		if className != "" { return }

		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())

	if className == "" {
		return "", nil
	}
	if namespace == "" {
		return className, nil
	}
	return namespace + "\\" + className, nil
}

type mutationInfo struct {
	line int
	code string
}

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
	container *SymfonyContainer
	auditor   *ServiceAuditor
}

func (v *PHPVisitor) walk(n *sitter.Node) {
	if n == nil {
		return
	}
	nodeType := n.Kind()

	oldClass, oldMethod, oldIsRes := v.curClass, v.curMethod, v.isReset

	switch nodeType {
	case "namespace_definition":
		if nameNode := n.ChildByFieldName("name"); nameNode != nil {
			v.namespace = v.getContent(nameNode)
		}

	case "class_declaration", "trait_declaration", "anonymous_class":
		if nameNode := n.ChildByFieldName("name"); nameNode != nil {
			v.curClass = v.getContent(nameNode)
		} else {
			v.curClass = "AnonymousClass"
		}

		// Track this class as audited
		fullName := v.curClass
		if v.namespace != "" {
			fullName = v.namespace + "\\" + v.curClass
		}
		v.auditor.mu.Lock()
		v.auditor.AuditedClasses[fullName] = true
		v.auditor.mu.Unlock()

		classText := strings.ToLower(string(v.content[n.StartByte():n.EndByte()]))
		v.isReset = strings.Contains(classText, "resetinterface") || strings.Contains(classText, "resettableinterface")
		
		v.mutated = make(map[string]mutationInfo)
		v.resetted = make(map[string]bool)

	case "method_declaration", "function_definition":
		if nameNode := n.ChildByFieldName("name"); nameNode != nil {
			v.curMethod = v.getContent(nameNode)
		}

	case "assignment_expression", "augmented_assignment_expression":
		v.handleMutation(n.ChildByFieldName("left"))

	case "update_expression":
		v.handleMutation(n)

	case "exit_statement", "exit":
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
		if v.isReset {
			v.performResetCheck()
		}
		v.curClass, v.isReset = oldClass, oldIsRes
	} else if nodeType == "method_declaration" || nodeType == "function_definition" {
		v.curMethod = oldMethod
	}
}

func (v *PHPVisitor) handleMutation(n *sitter.Node) {
	if n == nil || v.curMethod == "__construct" || (v.curMethod == "" && v.curClass != "AnonymousClass" && v.curClass != "") {
		return
	}

	fullName := v.curClass
	if v.namespace != "" {
		fullName = v.namespace + "\\" + v.curClass
	}

	// Skip if explicitly marked as non-shared in Symfony container OR if in SafeNamespaces
	if v.isExplicitlyNonShared(fullName) || v.isSafeNamespace(fullName) {
		return
	}

	switch n.Kind() {
	case "member_access_expression":
		obj := n.ChildByFieldName("object")
		if obj != nil && strings.Contains(v.getContent(obj), "$this") {
			nameNode := n.ChildByFieldName("name")
			if nameNode != nil {
				v.logMutation(n, v.getContent(nameNode), false)
			}
		}
	case "subscript_expression":
		if n.ChildCount() > 0 {
			v.handleMutation(n.Child(0))
		}
	case "scoped_property_access_expression":
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
	case "update_expression":
		for i := uint(0); i < n.ChildCount(); i++ {
			c := n.Child(i)
			if c.Kind() != "++" && c.Kind() != "--" {
				v.handleMutation(c)
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
		v.addFinding(n, msg, "", "ERROR")
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

func (v *PHPVisitor) isSafeNamespace(className string) bool {
	className = strings.TrimPrefix(className, "\\")
	for _, ns := range v.auditor.Config.SafeNamespaces {
		if strings.HasPrefix(className, strings.TrimPrefix(ns, "\\")) {
			return true
		}
	}
	return false
}

func (v *PHPVisitor) isExplicitlyNonShared(className string) bool {
	if v.container == nil {
		return false
	}
	className = strings.TrimPrefix(strings.ReplaceAll(className, "/", "\\"), "\\")
	for _, def := range v.container.Definitions {
		defClass := strings.TrimPrefix(def.Class, "\\")
		if defClass == className {
			return !def.Shared
		}
	}
	return false
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
