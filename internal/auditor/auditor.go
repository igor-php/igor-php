// Package main provides the core auditing logic for igor-php.
package auditor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/igor-php/igor-php/internal/analyzer"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// Auditor orchestrates the analysis of PHP files.
type Auditor struct {
	Config               config.Config
	Symfony              *SymfonyBridge
	NonSharedServices    analyzer.NonSharedServiceMap
	InterfaceAliases     analyzer.AliasesMap
	AuditedClasses       map[string]bool
	methodSignatureCache map[string]map[string]string
	callGraph            map[string]map[string]bool
	projectClasses       map[string]bool
	classParents         map[string]string
	declaredMethods      map[string]map[string]bool
	parentClassCache     map[string]string
	// classDecls caches the parsed declarations read by the type lookups, by normalized class name.
	classDecls map[string]classDecl
	// sharedTypes caches, by interface or abstract class, whether a shared service is of that type.
	sharedTypes          map[string]bool
	FileContentOverrides map[string][]byte
	classShapes          map[string]analyzer.ClassShape
	methodStates         map[string]map[string]analyzer.MethodState
	subclasses           map[string][]string
	// classFiles maps every audited class to its file: the closure rule reads the methods a
	// closure is passed to, in files the container map does not list.
	classFiles map[string]string
	// callables caches, by file, what the methods of its classes do with their parameters.
	callables map[string]map[string]analyzer.ClassCallables
	// composer is the project's Composer autoloader, read once to locate the other classes.
	composer *composerAutoload
	// userFunctions are the namespaced functions declared by the files Composer always loads.
	userFunctions map[string]bool
	// auditedFunctions are the namespaced functions declared by the audited files.
	auditedFunctions map[string]bool
	mu               sync.Mutex
}

// NewAuditor creates a new instance of the Auditor.
func NewAuditor(cfg config.Config) *Auditor {
	return &Auditor{
		Config:               cfg,
		AuditedClasses:       make(map[string]bool),
		methodSignatureCache: make(map[string]map[string]string),
		callGraph:            make(map[string]map[string]bool),
		projectClasses:       make(map[string]bool),
		classParents:         make(map[string]string),
		declaredMethods:      make(map[string]map[string]bool),
		parentClassCache:     make(map[string]string),
		FileContentOverrides: make(map[string][]byte),
	}
}

// LoadInterfaceAliases normalizes raw interface-to-target mappings and stores
// them as interface FQCN -> concrete class FQCN. When a Symfony container is
// available, service IDs and alias chains are resolved to their concrete class.
func (a *Auditor) LoadInterfaceAliases(raw analyzer.AliasesMap) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.InterfaceAliases == nil {
		a.InterfaceAliases = make(analyzer.AliasesMap)
	}

	for iface, target := range raw {
		iface = normalizeClassName(iface)
		target = normalizeClassName(target)
		if iface == "" || target == "" {
			continue
		}
		a.InterfaceAliases[iface] = a.resolveAliasToConcrete(target)
	}
}

// UseFramework plugs the detected framework bridge into the auditor.
func (a *Auditor) UseFramework(b *SymfonyBridge) {
	a.Symfony = b
	if b == nil {
		return
	}
	if b.Framework == FrameworkLaravel {
		a.Config.SafeNamespaces = append(a.Config.SafeNamespaces, LaravelSafeNamespaces...)
	}
	a.LoadSymfonyAliases()
}

// LoadSymfonyAliases converts the Symfony container alias map into the unified
// interface alias map used by reachability ranking.
func (a *Auditor) LoadSymfonyAliases() {
	if a.Symfony == nil || a.Symfony.Container == nil {
		return
	}

	raw := make(analyzer.AliasesMap)
	for aliasKey, val := range a.Symfony.Container.Aliases {
		if target := resolveAliasTarget(val); target != "" {
			raw[aliasKey] = target
		}
	}
	a.LoadInterfaceAliases(raw)
}

// resolveInterfaceClass returns the concrete class for an interface FQCN when
// an alias is known. It returns an empty string when no mapping exists.
func (a *Auditor) resolveInterfaceClass(className string) string {
	if concrete, ok := a.InterfaceAliases[normalizeClassName(className)]; ok {
		return concrete
	}
	return ""
}

// resolveAliasToConcrete follows Symfony aliases and definitions until it
// reaches a concrete class. For non-Symfony projects it returns the target as-is.
func (a *Auditor) resolveAliasToConcrete(target string) string {
	if a.Symfony == nil || a.Symfony.Container == nil {
		return target
	}

	possibleIDs := a.resolveAliases(target)
	for _, id := range possibleIDs {
		for defID, def := range a.Symfony.Container.Definitions {
			normDefID := normalizeClassName(defID)
			normDefClass := normalizeClassName(def.Class)
			if id == normDefID || id == normDefClass {
				if def.Class != "" {
					return normDefClass
				}
				return normDefID
			}
		}
	}
	return target
}

type fileEngine struct {
	*Auditor
	isVendor bool
	path     string
}

func (f *fileEngine) RecordClassAudited(name string) {
	f.Auditor.RecordClassAudited(name)
	f.Auditor.recordClassFile(name, f.path)
	if !f.isVendor {
		f.Auditor.recordProjectClass(name)
	}
}

func (a *Auditor) recordProjectClass(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.projectClasses[normalizeClassName(name)] = true
}

func (a *Auditor) RecordMethodCall(callerClass, callerMethod, calleeClass, calleeMethod string) {
	if callerMethod == "" || calleeMethod == "" {
		return
	}
	callerKey := normalizeClassName(callerClass) + "::" + callerMethod
	calleeKey := normalizeClassName(calleeClass) + "::" + calleeMethod

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.callGraph[callerKey] == nil {
		a.callGraph[callerKey] = make(map[string]bool)
	}
	a.callGraph[callerKey][calleeKey] = true
}

// RecordClassParent records a direct `extends` relationship (childClass extends
// parentClass), used to conservatively promote reachability to inherited methods.
func (a *Auditor) RecordClassParent(className, parentClassName string) {
	if className == "" || parentClassName == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.classParents[normalizeClassName(className)] = normalizeClassName(parentClassName)
}

// RecordMethodDeclared records that methodName has its own declaration (body) on
// className, distinguishing an override from an inherited, un-overridden method.
func (a *Auditor) RecordMethodDeclared(className, methodName string) {
	if className == "" || methodName == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := normalizeClassName(className)
	if a.declaredMethods[key] == nil {
		a.declaredMethods[key] = make(map[string]bool)
	}
	a.declaredMethods[key][methodName] = true
}

// resolveDeclaringAncestor walks the direct-`extends` parent chain starting at
// class, looking for the nearest ancestor (including class itself) that
// directly declares methodName. It stops as soon as it finds a declaration —
// so an override on a subclass is never skipped in favor of a grandparent's
// declaration — and guards against cycles in a malformed hierarchy. Returns
// "" if no class in the chain declares the method.
func (a *Auditor) resolveDeclaringAncestor(class, method string) string {
	visited := make(map[string]bool)
	for class != "" && !visited[class] {
		if a.declaredMethods[class][method] {
			return class
		}
		visited[class] = true
		class = a.classParents[class]
	}
	return ""
}

func (a *Auditor) MarkReachability(results []symbol.AuditStatus) {
	a.mu.Lock()
	graph := a.callGraph
	projectClasses := a.projectClasses
	a.mu.Unlock()

	reachableNodes := make(map[string]bool)
	var queue []string
	enqueue := func(node string) {
		if !reachableNodes[node] {
			reachableNodes[node] = true
			queue = append(queue, node)
		}
	}

	for callerKey := range graph {
		callerClass, _, found := strings.Cut(callerKey, "::")
		if found && projectClasses[callerClass] {
			enqueue(callerKey)
		}
	}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		for callee := range graph[node] {
			enqueue(callee)
		}

		class, method, found := strings.Cut(node, "::")
		if !found {
			continue
		}
		if concrete := a.resolveInterfaceClass(class); concrete != "" && concrete != class {
			enqueue(concrete + "::" + method)
		}
		if ancestor := a.resolveDeclaringAncestor(class, method); ancestor != "" && ancestor != class {
			enqueue(ancestor + "::" + method)
		}
	}

	for i := range results {
		for j := range results[i].Findings {
			finding := &results[i].Findings[j]
			if finding.ContextClass == "" || finding.ContextMethod == "" {
				continue
			}
			class := normalizeClassName(finding.ContextClass)
			reachable := reachableNodes[class+"::"+finding.ContextMethod]
			for _, method := range finding.AlsoMethods {
				reachable = reachable || reachableNodes[class+"::"+method]
			}
			if reachable {
				finding.Reachability = "HIGH"
			} else {
				finding.Reachability = "INFO"
			}
		}
	}
}

func (a *Auditor) SetFileOverride(path string, content []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.FileContentOverrides == nil {
		a.FileContentOverrides = make(map[string][]byte)
	}
	if abs, err := filepath.Abs(path); err == nil {
		a.FileContentOverrides[abs] = content
	}
	a.FileContentOverrides[filepath.Clean(path)] = content
}

func (a *Auditor) getFileContent(path string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.FileContentOverrides != nil {
		if content, ok := a.FileContentOverrides[filepath.Clean(path)]; ok {
			return content, nil
		}
		if abs, err := filepath.Abs(path); err == nil {
			if content, ok := a.FileContentOverrides[abs]; ok {
				return content, nil
			}
		}
	}
	return os.ReadFile(path)
}

// Audit analyzes a single PHP file and returns findings.
func (a *Auditor) Audit(path string, dependencies []string) ([]symbol.Finding, error) {
	content, err := a.getFileContent(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %v", path, err)
	}

	p := sitter.NewParser()
	lang := sitter.NewLanguage(php.LanguagePHP())
	if err := p.SetLanguage(lang); err != nil {
		return nil, fmt.Errorf("failed to set language for %s: %v", path, err)
	}

	tree := p.Parse(content, nil)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse %s", path)
	}
	defer tree.Close()

	if functions := analyzer.DeclaredFunctionsIn(tree.RootNode(), content); len(functions) > 0 {
		a.mu.Lock()
		if a.auditedFunctions == nil {
			a.auditedFunctions = make(map[string]bool)
		}
		for _, fn := range functions {
			a.auditedFunctions[strings.ToLower(normalizeClassName(fn))] = true
		}
		a.mu.Unlock()
	}

	v := analyzer.NewVisitor(content, &fileEngine{
		Auditor:  a,
		isVendor: isVendorPath(a.Config.NormalizePath(path)),
		path:     path,
	})
	v.SetDependencies(dependencies)
	v.SetNonSharedServices(a.NonSharedServices)
	if a.Symfony != nil {
		v.SetFramework(a.Symfony.Framework)
		if a.Symfony.Container != nil {
			v.SetPersistence(a.Symfony.Container.Persistence)
		}
	}
	v.Walk(tree.RootNode())

	return v.Findings(), nil
}

func isVendorPath(path string) bool {
	p := filepath.ToSlash(path)
	if strings.Contains(p, "/vendor/") || strings.HasPrefix(p, "vendor/") {
		return true
	}
	if abs, err := filepath.Abs(path); err == nil {
		if strings.Contains(filepath.ToSlash(abs), "/vendor/") {
			return true
		}
	}
	return false
}

// ExtractFQCN extracts the full name of the first class or trait declared in a file.
func (a *Auditor) ExtractFQCN(path string) (string, error) {
	fqcns, err := a.ExtractFQCNs(path)
	if err != nil || len(fqcns) == 0 {
		return "", err
	}
	return fqcns[0], nil
}

// ExtractFQCNs extracts the full names of every class and trait declared in a file, in declaration order.
func (a *Auditor) ExtractFQCNs(path string) ([]string, error) {
	content, err := a.getFileContent(path)
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

	var namespace string
	var fqcns []string
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "namespace_definition":
			namespace = ""
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				namespace = string(content[nameNode.StartByte():nameNode.EndByte()])
			}
		case "class_declaration", "trait_declaration":
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				className := string(content[nameNode.StartByte():nameNode.EndByte()])
				if namespace != "" {
					className = namespace + "\\" + className
				}
				fqcns = append(fqcns, className)
			}
			return
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())

	return fqcns, nil
}

func (a *Auditor) RecordClassAudited(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.AuditedClasses[name] = true
}

func (a *Auditor) IsSafeNamespace(className string) bool {
	className = strings.TrimPrefix(className, "\\")
	for _, ns := range a.Config.SafeNamespaces {
		if strings.HasPrefix(className, strings.TrimPrefix(ns, "\\")) {
			return true
		}
	}
	return false
}

func normalizeClassName(s string) string {
	s = strings.ReplaceAll(s, "/", "\\")
	s = strings.TrimSpace(s)
	return strings.TrimPrefix(s, "\\")
}

func resolveAliasTarget(val interface{}) string {
	if s, ok := val.(string); ok {
		return s
	}
	if m, ok := val.(map[string]interface{}); ok {
		for _, key := range []string{"service", "id", "target"} {
			if target, ok := m[key].(string); ok {
				return target
			}
		}
	}
	return ""
}

func (a *Auditor) resolveAliases(className string) []string {
	if a.Symfony == nil || a.Symfony.Container == nil {
		return []string{normalizeClassName(className)}
	}

	normClassName := normalizeClassName(className)
	possibleIDs := []string{normClassName}
	visited := map[string]bool{normClassName: true}
	current := normClassName

	for {
		var foundTarget string
		for aliasKey, val := range a.Symfony.Container.Aliases {
			if normalizeClassName(aliasKey) == current {
				target := resolveAliasTarget(val)
				if target != "" {
					foundTarget = normalizeClassName(target)
					break
				}
			}
		}

		if foundTarget != "" {
			current = foundTarget
			if !visited[current] {
				visited[current] = true
				possibleIDs = append(possibleIDs, current)
				continue
			}
		}
		break
	}

	return possibleIDs
}

func (a *Auditor) IsExplicitlyNonShared(className string) bool {
	if a.Symfony == nil || a.Symfony.Container == nil {
		return false
	}
	possibleIDs := a.resolveAliases(className)

	hasShared := false
	hasNonShared := false

	for defID, def := range a.Symfony.Container.Definitions {
		normDefID := normalizeClassName(defID)
		normDefClass := normalizeClassName(def.Class)

		for _, id := range possibleIDs {
			if normDefID == id || normDefClass == id {
				if def.IsExcluded() {
					continue
				}
				if def.Shared {
					hasShared = true
				} else {
					hasNonShared = true
				}
			}
		}
	}

	// If ANY matching definition is shared, it is NOT explicitly non-shared (it can persist in worker memory)
	if hasShared {
		return false
	}
	return hasNonShared
}

func (a *Auditor) IsResettable(className string) bool {
	if a.Symfony == nil || a.Symfony.Container == nil {
		return false
	}
	possibleIDs := a.resolveAliases(className)

	// Check if any of possibleIDs corresponds to a Doctrine Manager
	isDoctrine := false
	for _, id := range possibleIDs {
		lower := strings.ToLower(id)
		if strings.Contains(lower, "doctrine\\orm\\entitymanager") ||
			strings.Contains(lower, "doctrine\\persistence\\objectmanager") ||
			strings.Contains(lower, "doctrine\\odm\\mongodb\\documentmanager") {
			isDoctrine = true
			break
		}
	}

	if isDoctrine {
		if def, exists := a.Symfony.Container.Definitions["doctrine"]; exists && def.IsResettable() {
			return true
		}
		if def, exists := a.Symfony.Container.Definitions["doctrine_mongodb"]; exists && def.IsResettable() {
			return true
		}
	}

	for defID, def := range a.Symfony.Container.Definitions {
		normDefID := normalizeClassName(defID)
		normDefClass := normalizeClassName(def.Class)

		for _, id := range possibleIDs {
			if normDefID == id || normDefClass == id {
				if def.IsResettable() {
					return true
				}
			}
		}
	}
	return false
}

// HasServiceDefinition reports whether the framework service map defines a service of this class,
// in which case it, not the class header, says whether the container resets the service.
func (a *Auditor) HasServiceDefinition(className string) bool {
	if a.Symfony == nil || a.Symfony.Container == nil {
		return false
	}
	normClass := normalizeClassName(className)
	for _, def := range a.Symfony.Container.Definitions {
		if normalizeClassName(def.Class) == normClass && !def.IsExcluded() {
			return true
		}
	}
	return false
}

// MissingResetTag reports whether the Symfony container defines shared services of this class and
// resets none of them: no definition is tagged kernel.reset, and neither is the decorator that took
// over the tags of a decorated one (DecoratorServicePass moves them to the outermost decorator).
func (a *Auditor) MissingResetTag(className string) bool {
	if a.Symfony == nil || a.Symfony.Container == nil || a.Symfony.Framework != FrameworkSymfony {
		return false
	}
	normClass := normalizeClassName(className)
	found := false
	for id, def := range a.Symfony.Container.Definitions {
		if normalizeClassName(def.Class) != normClass || !def.Shared || def.IsExcluded() {
			continue
		}
		if def.IsResettable() || a.isResetByDecorator(id) {
			return false
		}
		found = true
	}
	return found
}

// isResetByDecorator reports whether a decorated service is reset through its decorator:
// DecoratorServicePass moves kernel.reset to the outermost decorator, which names the service
// it decorates. Maps that predate that export are matched through the "<decorator>.inner" ids.
func (a *Auditor) isResetByDecorator(id string) bool {
	for _, def := range a.Symfony.Container.Definitions {
		if def.Decorates == id && def.IsResettable() {
			return true
		}
	}
	for strings.HasSuffix(id, ".inner") {
		id = strings.TrimSuffix(id, ".inner")
		if def, exists := a.Symfony.Container.Definitions[id]; exists && def.IsResettable() {
			return true
		}
	}
	return false
}

// IsSharedService reports whether an object of type className may be a shared service: a class with
// a shared definition, or an interface or abstract class that the container aliases to a shared
// service, or that a shared service implements or extends. A getter declared to return
// FactoryRegistryInterface or an abstract Factory can return a shared service: only a concrete
// class without a shared definition names a per-call object.
func (a *Auditor) IsSharedService(className string) bool {
	if a.Symfony == nil {
		return true
	}
	if a.Symfony.IsSharedService(className) {
		return true
	}
	if a.Symfony.Container == nil {
		return false
	}
	name := normalizeClassName(className)
	a.mu.Lock()
	shared, cached := a.sharedTypes[name]
	a.mu.Unlock()
	if !cached {
		shared = a.sharedSubtype(name)
		a.mu.Lock()
		if a.sharedTypes == nil {
			a.sharedTypes = make(map[string]bool)
		}
		a.sharedTypes[name] = shared
		a.mu.Unlock()
	}
	return shared
}

// sharedSubtype reports whether the interface or abstract class t is aliased to a shared service,
// or implemented or extended by one.
func (a *Auditor) sharedSubtype(t string) bool {
	// An alias needs no declaration: an internal interface (\Iterator) has no source file
	defs := a.Symfony.Container.Definitions
	for id, target := range a.Symfony.Container.Aliases {
		if strings.EqualFold(normalizeClassName(id), t) {
			if def, exists := defs[resolveAliasTarget(target)]; exists && def.Shared && !def.IsExcluded() {
				return true
			}
		}
	}
	decl, ok := a.classDeclaration(t)
	if !ok || !decl.abstract {
		return false
	}
	for _, def := range defs {
		if !def.Shared || def.IsExcluded() || def.Class == "" {
			continue
		}
		if a.isSubtype(normalizeClassName(def.Class), t, map[string]bool{}) {
			return true
		}
	}
	return false
}

// isSubtype reports whether class is ancestor, or extends or implements it.
func (a *Auditor) isSubtype(class, ancestor string, visited map[string]bool) bool {
	if strings.EqualFold(class, ancestor) {
		return true
	}
	if class == "" || visited[class] {
		return false
	}
	visited[class] = true
	decl, ok := a.classDeclaration(class)
	if !ok {
		return false
	}
	for _, super := range append([]string{decl.parent}, decl.supertypes...) {
		if super != "" && a.isSubtype(normalizeClassName(super), ancestor, visited) {
			return true
		}
	}
	return false
}

func isDevPackageMatch(relPathLower string, devPackages []string) bool {
	if !strings.HasPrefix(relPathLower, "vendor/") && relPathLower != "vendor" {
		return false
	}
	for _, pkg := range devPackages {
		pkgLower := strings.ToLower(pkg)
		target := "vendor/" + pkgLower
		if strings.HasPrefix(relPathLower, target+"/") || relPathLower == target {
			return true
		}
	}
	return false
}

// IsDevPackagePath returns true if the file path belongs to a dev package in the vendor/ directory.
// An optional rootPath anchors the check to the project's actual Composer vendor directory.
func (a *Auditor) IsDevPackagePath(path string, rootPath ...string) bool {
	path = a.Config.NormalizePath(path)

	var root string
	if len(rootPath) > 0 && rootPath[0] != "" {
		root = rootPath[0]
	} else if a.Symfony != nil && a.Symfony.Root != "" {
		root = a.Symfony.Root
	}

	if root != "" {
		if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
			return isDevPackageMatch(strings.ToLower(filepath.ToSlash(rel)), a.Config.DevPackages)
		}
	}

	// Fallback when root is unknown or path is outside root
	pathLower := strings.ToLower(filepath.ToSlash(path))
	if isDevPackageMatch(pathLower, a.Config.DevPackages) {
		return true
	}

	for _, pkg := range a.Config.DevPackages {
		pkgLower := strings.ToLower(pkg)
		target := "/vendor/" + pkgLower
		if strings.Contains(pathLower, target+"/") || strings.HasSuffix(pathLower, target) {
			return true
		}
	}
	return false
}

// GetMethodReturnType returns the declared return type of a method on a class (including inherited methods from parent classes).
func (a *Auditor) GetMethodReturnType(className, methodName string) string {
	return a.getMethodReturnTypeWithDepth(className, methodName, make(map[string]bool))
}

func (a *Auditor) getMethodReturnTypeWithDepth(className, methodName string, visited map[string]bool) string {
	normClass := normalizeClassName(className)
	if visited[normClass] {
		return ""
	}
	visited[normClass] = true

	decl, ok := a.classDeclaration(normClass)
	if !ok {
		return ""
	}
	if retType := decl.methods[methodName]; retType != "" {
		return retType
	}
	// A trait method takes precedence over the parent's
	traits, traitMethodName := decl.traits, methodName
	if alias, ok := decl.aliases[methodName]; ok {
		traitMethodName = alias.method
		if alias.trait != "" {
			traits = []string{alias.trait}
		}
	} else if chosen, ok := decl.insteadof[methodName]; ok {
		traits = []string{chosen}
	}
	if retType, found := a.fromTraits(normClass, traits, func(trait string) string {
		return a.getMethodReturnTypeWithDepth(trait, traitMethodName, visited)
	}); found {
		return retType
	}
	if decl.parent != "" {
		return a.getMethodReturnTypeWithDepth(decl.parent, methodName, visited)
	}
	return ""
}

// GetPropertyType returns the declared type (an FQCN, or a scalar or union as written) of a property
// a class declares or inherits: from its traits, then from its parents. ConstraintValidator's
// $context and DenormalizerAwareTrait's $denormalizer are declared outside the class using them.
func (a *Auditor) GetPropertyType(className, propertyName string) string {
	return a.propertyTypeWithDepth(normalizeClassName(className), propertyName, map[string]bool{}, false)
}

// propertyTypeWithDepth looks prop up in class, its traits, then its parents. inherited is true in
// the parents, where a private property is not the one the subclass reads.
func (a *Auditor) propertyTypeWithDepth(class, prop string, visited map[string]bool, inherited bool) string {
	if class == "" || visited[class] {
		return ""
	}
	visited[class] = true
	decl, ok := a.classDeclaration(class)
	if !ok {
		return ""
	}
	if t, declared := decl.props[prop]; declared && !(inherited && decl.privateProps[prop]) {
		// Declared here, typed or not: a parent's property of the same name is not this one
		return t
	}
	if t, found := a.fromTraits(class, decl.traits, func(trait string) string {
		return a.propertyTypeWithDepth(normalizeClassName(trait), prop, visited, inherited)
	}); found {
		return t
	}
	return a.propertyTypeWithDepth(normalizeClassName(decl.parent), prop, visited, true)
}

// fromTraits returns the type lookup finds in the traits class uses. self and static in a trait
// name the class using it. found is true as soon as a trait declares the member: two traits
// declaring it with different types (a conflict solved by insteadof) give "", the type is unknown.
func (a *Auditor) fromTraits(class string, traits []string, lookup func(trait string) string) (string, bool) {
	var result string
	found := false
	for _, trait := range traits {
		t := lookup(trait)
		if t == "" {
			continue
		}
		if strings.EqualFold(normalizeClassName(t), normalizeClassName(trait)) {
			// A trait is not a type: its self
			t = class
		}
		if found && !strings.EqualFold(t, result) {
			return "", true
		}
		result, found = t, true
	}
	return result, found
}

// classDeclaration parses the file declaring class (located through the framework class map, the
// audited files, then Composer) once, and caches what the type lookups read from it.
func (a *Auditor) classDeclaration(class string) (classDecl, bool) {
	a.mu.Lock()
	if decl, cached := a.classDecls[class]; cached {
		a.mu.Unlock()
		return decl, decl.methods != nil
	}
	a.mu.Unlock()

	var decl classDecl
	if filePath := a.classFile(class); filePath != "" {
		if parsed, err := a.parseClassDecl(class, filePath); err == nil {
			decl = parsed
		}
	}
	if decl.methods == nil {
		// Cache the miss too: the file is not parsed again
		decl = classDecl{}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.classDecls == nil {
		a.classDecls = make(map[string]classDecl)
	}
	a.classDecls[class] = decl
	a.methodSignatureCache[class] = decl.methods
	if decl.methods == nil {
		a.methodSignatureCache[class] = map[string]string{}
	}
	if decl.parent != "" {
		a.parentClassCache[class] = decl.parent
	}
	return decl, decl.methods != nil
}

func isBuiltinType(t string) bool {
	switch strings.ToLower(t) {
	case "void", "int", "string", "bool", "float", "array", "callable", "object", "mixed", "never", "false", "null", "true", "iterable":
		return true
	}
	return false
}

// parseMethodDeclaration returns the name of a method and its return type, resolved against the
// namespace and the imports of the file declaring it (lines).
func parseMethodDeclaration(node *sitter.Node, content []byte, lines []string, namespace, className string) (string, string) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return "", ""
	}
	mName := string(content[nameNode.StartByte():nameNode.EndByte()])
	var retType string
	if retNode := node.ChildByFieldName("return_type"); retNode != nil {
		retType = resolveDeclaredType(string(content[retNode.StartByte():retNode.EndByte()]), lines, namespace, className)
	}
	return mName, retType
}

// resolveDeclaredType resolves a type written in a declaration (?Foo, \Foo\Bar, Foo, self) against the
// imports and namespace of its file. Unions, intersections and scalar types are returned as written.
func resolveDeclaredType(raw string, lines []string, namespace, className string) string {
	t := strings.TrimPrefix(strings.TrimSpace(raw), "?")
	qualified := strings.HasPrefix(t, "\\")
	t = strings.TrimPrefix(t, "\\")
	switch {
	case t == "self" || t == "static":
		return className
	case isBuiltinType(t) || strings.ContainsAny(t, "|&()"):
		// A union or intersection names no single class
		return t
	case qualified:
		// Fully qualified: \Doctrine\ORM\EntityManagerInterface
		return t
	}
	// EntityManagerInterface comes from a use import of this file, not from its namespace
	return resolveTypeInFile(t, lines, namespace)
}

// classDecl is what the type lookups read from the declaration of a class, an interface or a trait.
type classDecl struct {
	// methods are the return types of its methods, by name.
	methods map[string]string
	parent  string
	traits  []string
	// props are the declared types of its properties (promoted constructor parameters included),
	// "" for an untyped one.
	props map[string]string
	// privateProps are its private properties, which its subclasses do not inherit.
	privateProps map[string]bool
	// abstract is true for an interface or an abstract class: no object has exactly this type.
	abstract bool
	// supertypes are the interfaces it implements, and the interfaces an interface extends.
	supertypes []string
	// insteadof gives the trait a method is taken from when traits conflict (B::current insteadof A).
	insteadof map[string]string
	// aliases are the methods renamed from a trait (A::current as oldCurrent).
	aliases map[string]traitMethod
}

// traitMethod is a method of a trait, or of any trait the class uses when trait is "".
type traitMethod struct {
	trait, method string
}

func (a *Auditor) parseClassDecl(className, filePath string) (classDecl, error) {
	decl := classDecl{methods: map[string]string{}, props: map[string]string{}}
	content, err := a.getFileContent(filePath)
	if err != nil {
		return decl, err
	}

	p := sitter.NewParser()
	lang := sitter.NewLanguage(php.LanguagePHP())
	_ = p.SetLanguage(lang)

	tree := p.Parse(content, nil)
	if tree == nil {
		return decl, fmt.Errorf("failed to parse %s", filePath)
	}
	defer tree.Close()

	var namespace string
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		kind := n.Kind()
		if kind == "namespace_definition" {
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				namespace = string(content[nameNode.StartByte():nameNode.EndByte()])
			}
		}

		if kind == "class_declaration" || kind == "interface_declaration" || kind == "trait_declaration" {
			var declName string
			if nameNode := n.ChildByFieldName("name"); nameNode != nil {
				declName = string(content[nameNode.StartByte():nameNode.EndByte()])
			}

			fqcn := declName
			if namespace != "" {
				fqcn = namespace + "\\" + declName
			}

			if normalizeClassName(fqcn) == normalizeClassName(className) {
				// Only the imports of the namespace declaring the class apply to it, and they come
				// before it: "use SomeTrait;" in its body is not an import
				lines := importLines(n, content)
				decl.parent = resolveParentClassFromNode(n, content, lines, namespace)
				readSupertypes(n, content, lines, namespace, &decl)
				readClassMembers(n, content, lines, namespace, className, &decl)
				return
			}
		}

		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())

	return decl, nil
}

// readClassMembers reads the methods, properties and used traits of a class-like declaration.
func readClassMembers(n *sitter.Node, content []byte, lines []string, namespace, className string, decl *classDecl) {
	text := func(x *sitter.Node) string { return string(content[x.StartByte():x.EndByte()]) }
	body := n.ChildByFieldName("body")
	if body == nil {
		return
	}
	for i := uint(0); i < body.NamedChildCount(); i++ {
		member := body.NamedChild(i)
		switch member.Kind() {
		case "use_declaration":
			readTraitUse(member, content, lines, namespace, decl)
		case "property_declaration":
			typeNode := member.ChildByFieldName("type")
			for j := uint(0); j < member.NamedChildCount(); j++ {
				element := member.NamedChild(j)
				if element.Kind() != "property_element" {
					continue
				}
				if name := element.ChildByFieldName("name"); name != nil {
					decl.recordProp(member, text(name), typeNode, content, lines, namespace, className)
				}
			}
		case "method_declaration":
			mName, retType := parseMethodDeclaration(member, content, lines, namespace, className)
			if mName == "" {
				continue
			}
			decl.methods[mName] = retType
			if !strings.EqualFold(mName, "__construct") {
				continue
			}
			if params := member.ChildByFieldName("parameters"); params != nil {
				for j := uint(0); j < params.NamedChildCount(); j++ {
					param := params.NamedChild(j)
					typeNode, name := param.ChildByFieldName("type"), param.ChildByFieldName("name")
					if param.Kind() == "property_promotion_parameter" && name != nil {
						decl.recordProp(param, text(name), typeNode, content, lines, namespace, className)
					}
				}
			}
		}
	}
}

// recordProp records a property declared by member (a property declaration or a promoted
// parameter): its type, "" when it has none, and whether it is private.
func (decl *classDecl) recordProp(member *sitter.Node, name string, typeNode *sitter.Node, content []byte, lines []string, namespace, className string) {
	name = strings.TrimPrefix(name, "$")
	var t string
	if typeNode != nil {
		t = resolveDeclaredType(string(content[typeNode.StartByte():typeNode.EndByte()]), lines, namespace, className)
	}
	decl.props[name] = t
	for i := uint(0); i < member.NamedChildCount(); i++ {
		if c := member.NamedChild(i); c.Kind() == "visibility_modifier" && strings.EqualFold(string(content[c.StartByte():c.EndByte()]), "private") {
			if decl.privateProps == nil {
				decl.privateProps = map[string]bool{}
			}
			decl.privateProps[name] = true
		}
	}
}

// readTraitUse reads the traits of a use declaration in a class body, and its conflict resolution
// and aliases: use A, B { B::current insteadof A; A::current as oldCurrent; }
func readTraitUse(use *sitter.Node, content []byte, lines []string, namespace string, decl *classDecl) {
	text := func(x *sitter.Node) string { return string(content[x.StartByte():x.EndByte()]) }
	traitName := func(x *sitter.Node) string { return resolveDeclaredType(text(x), lines, namespace, "") }
	for j := uint(0); j < use.NamedChildCount(); j++ {
		child := use.NamedChild(j)
		switch child.Kind() {
		case "name", "qualified_name":
			decl.traits = append(decl.traits, traitName(child))
		case "use_list":
			for k := uint(0); k < child.NamedChildCount(); k++ {
				rule := child.NamedChild(k)
				first, last := rule.NamedChild(0), rule.NamedChild(rule.NamedChildCount()-1)
				if first == nil || last == nil {
					continue
				}
				var source traitMethod
				if first.Kind() == "class_constant_access_expression" && first.NamedChildCount() == 2 {
					source = traitMethod{trait: traitName(first.NamedChild(0)), method: text(first.NamedChild(1))}
				} else {
					source.method = text(first)
				}
				switch {
				case rule.Kind() == "use_instead_of_clause" && source.trait != "":
					if decl.insteadof == nil {
						decl.insteadof = map[string]string{}
					}
					decl.insteadof[source.method] = source.trait
				case rule.Kind() == "use_as_clause" && last.Kind() == "name" && last != first:
					// "as protected" alone changes the visibility, not the name
					if decl.aliases == nil {
						decl.aliases = map[string]traitMethod{}
					}
					decl.aliases[text(last)] = source
				}
			}
		}
	}
}

// readSupertypes reads whether a declaration is abstract (an interface or an abstract class), and
// the interfaces it implements or extends.
func readSupertypes(n *sitter.Node, content []byte, lines []string, namespace string, decl *classDecl) {
	decl.abstract = n.Kind() == "interface_declaration"
	for i := uint(0); i < n.ChildCount(); i++ {
		child := n.Child(i)
		switch {
		case child.Kind() == "abstract_modifier":
			decl.abstract = true
		case child.Kind() == "class_interface_clause" || (child.Kind() == "base_clause" && n.Kind() == "interface_declaration"):
			for j := uint(0); j < child.NamedChildCount(); j++ {
				if t := child.NamedChild(j); t.Kind() == "name" || t.Kind() == "qualified_name" {
					decl.supertypes = append(decl.supertypes, resolveDeclaredType(string(content[t.StartByte():t.EndByte()]), lines, namespace, ""))
				}
			}
		}
	}
}

// resolveParentClassFromNode extracts and resolves the parent class name from a class declaration node.
func resolveParentClassFromNode(n *sitter.Node, content []byte, lines []string, namespace string) string {
	var parentClass string
	for i := uint(0); i < n.ChildCount(); i++ {
		child := n.Child(i)
		if child.Kind() == "base_clause" {
			for j := uint(0); j < child.ChildCount(); j++ {
				grandChild := child.Child(j)
				if grandChild.Kind() == "name" || grandChild.Kind() == "qualified_name" {
					parentClass = string(content[grandChild.StartByte():grandChild.EndByte()])
					break
				}
			}
			break
		}
	}

	if parentClass != "" {
		return resolveFQCNInFile(parentClass, lines, namespace)
	}
	return ""
}

// importLines returns the namespace imports (use Foo\Bar;) that apply to a declaration: those of
// its namespace written before it, one per line.
func importLines(decl *sitter.Node, content []byte) []string {
	start, _ := namespaceRange(decl, content)
	root := decl
	for root.Parent() != nil {
		root = root.Parent()
	}
	// The namespace imports only: not the "use SomeTrait;" lines in the body of a class before it
	var lines []string
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		for i := uint(0); i < n.ChildCount(); i++ {
			c := n.Child(i)
			if c.StartByte() >= decl.StartByte() {
				return
			}
			if c.EndByte() <= start {
				continue
			}
			switch c.Kind() {
			case "namespace_use_declaration":
				lines = append(lines, expandImport(string(content[c.StartByte():c.EndByte()]))...)
			case "namespace_definition", "compound_statement":
				walk(c)
			}
		}
	}
	walk(root)
	return lines
}

// expandImport returns one "use Foo\Bar;" or "use Foo\Bar as Baz;" line per class a use statement
// imports: a list (use A, B;) and a group (use App\State\{Registry, Cache as C};) give several,
// even written over several lines. Functions and constants import no class.
func expandImport(stmt string) []string {
	stmt = strings.Join(strings.Fields(stmt), " ")
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(stmt, "use "), ";"))
	if strings.HasPrefix(body, "function ") || strings.HasPrefix(body, "const ") {
		return nil
	}
	prefix := ""
	if open := strings.Index(body, "{"); open >= 0 {
		prefix = strings.TrimPrefix(strings.TrimSpace(body[:open]), "\\")
		body = strings.TrimSuffix(strings.TrimSpace(body[open+1:]), "}")
	}
	var lines []string
	for _, item := range strings.Split(body, ",") {
		if item = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(item), "\\")); item != "" {
			lines = append(lines, "use "+prefix+item+";")
		}
	}
	return lines
}

// namespaceRange returns the byte range of the namespace a declaration belongs to.
func namespaceRange(decl *sitter.Node, content []byte) (uint, uint) {
	start, end := uint(0), uint(len(content))
	for p := decl.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == "namespace_definition" && p.ChildByFieldName("body") != nil {
			return p.StartByte(), p.EndByte()
		}
		if p.Parent() == nil {
			// The program: find the namespace statements around the declaration
			for i := uint(0); i < p.ChildCount(); i++ {
				c := p.Child(i)
				if c.Kind() != "namespace_definition" {
					continue
				}
				if c.StartByte() <= decl.StartByte() {
					start = c.StartByte()
				} else if c.StartByte() < end {
					end = c.StartByte()
					break
				}
			}
		}
	}
	return start, end
}

// resolveTypeInFile resolves a type name written in a file: an imported name or alias, a qualified
// name whose first segment is imported (Mapping\ClassMetadata), or a name of the file's namespace.
func resolveTypeInFile(typeName string, lines []string, namespace string) string {
	first, rest, qualified := strings.Cut(typeName, "\\")
	if !qualified {
		return resolveFQCNInFile(typeName, lines, namespace)
	}
	if resolved := resolveFQCNInFile(first, lines, ""); resolved != first {
		return resolved + "\\" + rest
	}
	if namespace != "" {
		return namespace + "\\" + typeName
	}
	return typeName
}

// resolveFQCNInFile resolves class names against the namespace and "use" imports inside a file's lines.
func resolveFQCNInFile(typeName string, lines []string, namespace string) string {
	if strings.HasPrefix(typeName, "\\") {
		return strings.TrimPrefix(typeName, "\\")
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "use ") && strings.HasSuffix(line, ";") {
			importPart := strings.TrimSuffix(strings.TrimPrefix(line, "use "), ";")
			importPart = strings.TrimSpace(importPart)
			if strings.Contains(importPart, " as ") {
				parts := strings.Split(importPart, " as ")
				if len(parts) == 2 {
					fqcn := strings.TrimSpace(parts[0])
					alias := strings.TrimSpace(parts[1])
					if alias == typeName {
						return fqcn
					}
				}
			} else {
				parts := strings.Split(importPart, "\\")
				lastPart := parts[len(parts)-1]
				if lastPart == typeName {
					return importPart
				}
			}
		}
	}

	if namespace != "" {
		return namespace + "\\" + typeName
	}
	return typeName
}

// Extends reports whether class is ancestor or inherits from it. It follows the `extends`
// relationships recorded during the audit, and reads the parents of the classes outside the audit
// (an intermediate base kernel in a single-file audit) from their files in the framework class map.
func (a *Auditor) Extends(class, ancestor string) bool {
	class, ancestor = normalizeClassName(class), normalizeClassName(ancestor)
	visited := make(map[string]bool)
	for class != "" && !visited[class] {
		if strings.EqualFold(class, ancestor) {
			return true
		}
		visited[class] = true
		class = a.parentOf(class)
	}
	return false
}

// parentOf returns the parent of a class: the one recorded during the audit, or the one declared in
// its file, which the method signature lookup parses and caches.
func (a *Auditor) parentOf(class string) string {
	a.mu.Lock()
	parent, recorded := a.classParents[class]
	if !recorded {
		parent, recorded = a.parentClassCache[class]
	}
	_, parsed := a.methodSignatureCache[class]
	a.mu.Unlock()
	if recorded || parsed {
		return normalizeClassName(parent)
	}
	// No method has an empty name: the lookup only parses the class file and caches its parent
	a.getMethodReturnTypeWithDepth(class, "", map[string]bool{})
	a.mu.Lock()
	defer a.mu.Unlock()
	return normalizeClassName(a.parentClassCache[class])
}
