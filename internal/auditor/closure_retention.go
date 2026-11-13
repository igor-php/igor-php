package auditor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/igor-php/igor-php/internal/analyzer"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// retention is what a method does with a closure it is given.
type retention int

const (
	// retentionNone: the method calls the closure, or ignores it, and returns.
	retentionNone retention = iota
	// retentionKept: the method stores the closure somewhere that outlives the call.
	retentionKept
	// retentionUnknown: the method could not be read to the end.
	retentionUnknown
	// retentionHeld: a constructor stores the closure in the object it builds, and nowhere else:
	// the closure lives as long as that object.
	retentionHeld
)

// maxRetentionNodes bounds the methods followed to decide one call.
const maxRetentionNodes = 200

// unreadClosureHint replaces the remediation of a finding igor could not confirm.
const unreadClosureHint = "Igor could not read %s to tell whether it keeps the closure or only calls it. If it only calls it, add the finding to the baseline; if it keeps it, register the callback once at boot instead."

func (a *Auditor) recordClassFile(class, path string) {
	if class == "" || path == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.classFiles == nil {
		a.classFiles = make(map[string]string)
	}
	a.classFiles[strings.ToLower(normalizeClassName(class))] = path
}

// ResolveClosureLeaks reads the method each closure state leak finding passes its closure to.
// A method that only calls the closure cannot leak it: the finding is removed. A method that
// stores it keeps its finding. When the method cannot be read, a name that registers a callback
// (addListener()) keeps the error and any other becomes a warning. It reports whether it changed
// anything.
func (a *Auditor) ResolveClosureLeaks(results []symbol.AuditStatus) bool {
	// Files may have changed since the last audit of a long-lived process (igor mcp)
	a.mu.Lock()
	a.callables = nil
	a.userFunctions = nil
	a.mu.Unlock()

	changed := false
	for i := range results {
		kept := results[i].Findings[:0]
		for _, f := range results[i].Findings {
			ref := f.ClosureArg
			if ref == nil || f.Rule != symbol.RuleClosureStateLeak {
				kept = append(kept, f)
				continue
			}
			verdict, unread, class := a.closureVerdict(ref, f.ContextClass)
			switch verdict {
			case retentionNone:
				changed = true
				continue
			case retentionUnknown:
				if !isRegistrationName(outermost(ref).Method) && f.Severity == "ERROR" {
					f.Severity = "WARNING"
					f.Remediation = strings.Replace(unreadClosureHint, "%s", describeCall(unread, class), 1)
					changed = true
				}
			}
			kept = append(kept, f)
		}
		results[i].Findings = kept
	}
	return changed
}

// closureVerdict tells what happens to the closure of ref: kept, dropped, or unknown, with the
// call igor could not read and the class it resolved. A closure given to a constructor is judged
// there first: if the new object only holds it, what the outer call does with the object decides.
func (a *Auditor) closureVerdict(ref *symbol.ClosureArgRef, callerClass string) (retention, *symbol.ClosureArgRef, string) {
	return a.verdictOf(ref, callerClass, false)
}

// verdictOf is closureVerdict for ref. carrier is true when ref receives an object holding the
// closure rather than the closure itself.
func (a *Auditor) verdictOf(ref *symbol.ClosureArgRef, callerClass string, carrier bool) (retention, *symbol.ClosureArgRef, string) {
	verdict, class := a.closureRetention(ref, callerClass)
	if verdict == retentionUnknown && isSynchronousCallback(ref.Class, class, ref.Method) {
		verdict = retentionNone
	}
	if carrier && ref.Outer != nil && verdict == retentionNone {
		// new Envelope(new Box($closure)): Envelope may copy what the box holds ($box->callback),
		// which the summary does not follow. The envelope may hold the closure: the call decides
		verdict = retentionHeld
	}
	switch {
	case ref.Outer == nil && verdict == retentionHeld:
		return retentionUnknown, ref, class
	case ref.Outer == nil, verdict == retentionNone, verdict == retentionKept:
		return verdict, ref, class
	}
	outer, unread, outerClass := a.verdictOf(ref.Outer, callerClass, true)
	if verdict == retentionHeld {
		return outer, unread, outerClass
	}
	// The constructor could not be read: the object may hold the closure, so a call that keeps
	// the object keeps it too
	if outer == retentionKept {
		return retentionKept, unread, outerClass
	}
	return retentionUnknown, ref, class
}

// outermost returns the call made on the dependency, for a closure given to a constructor.
func outermost(ref *symbol.ClosureArgRef) *symbol.ClosureArgRef {
	for ref.Outer != nil {
		ref = ref.Outer
	}
	return ref
}

func describeCall(ref *symbol.ClosureArgRef, class string) string {
	if class == "" {
		class = ref.Class
	}
	if class == "" {
		return ref.Method + "()"
	}
	return class + "::" + ref.Method + "()"
}

// registrationPrefixes are the verbs of methods that keep the callback they are given.
var registrationPrefixes = []string{
	"add", "register", "on", "listen", "subscribe", "attach", "push", "bind", "extend", "set",
	"append", "prepend", "queue", "enqueue", "defer", "schedule", "hook", "observe", "macro",
	"resolving", "afterResolving", "beforeResolving", "terminating", "booted", "booting", "singleton",
	"scoped", "store", "put", "insert", "connect", "use", "pipe", "through", "middleware",
}

// isRegistrationName reports a method name that suggests the callback is kept.
func isRegistrationName(method string) bool {
	lower := strings.ToLower(method)
	for _, prefix := range registrationPrefixes {
		p := strings.ToLower(prefix)
		if !strings.HasPrefix(lower, p) {
			continue
		}
		if len(method) == len(p) {
			return true
		}
		if next := method[len(p)]; (next >= 'A' && next <= 'Z') || next == '_' {
			return true
		}
	}
	return false
}

// synchronousCallbacks are methods known to call their callback before returning, for the
// receivers igor cannot read: an interface with no container to resolve it, or an implementation
// that goes through a trait or a static helper. Each entry is a class or namespace prefix
// (lowercased) and the method names.
var synchronousCallbacks = []struct {
	prefix  string
	methods map[string]bool
}{
	{"doctrine\\orm\\entitymanager", map[string]bool{"wrapintransaction": true, "transactional": true}},
	{"doctrine\\dbal\\connection", map[string]bool{"transactional": true}},
	{"doctrine\\persistence\\objectmanager", map[string]bool{"wrapintransaction": true, "transactional": true}},
	{"symfony\\contracts\\cache\\", map[string]bool{"get": true}},
	{"symfony\\component\\cache\\", map[string]bool{"get": true}},
	{"psr\\cache\\", map[string]bool{"get": true}},
	{"illuminate\\database\\", map[string]bool{"transaction": true}},
	{"illuminate\\contracts\\cache\\", map[string]bool{"remember": true, "rememberforever": true, "sear": true, "flexible": true}},
	{"illuminate\\cache\\", map[string]bool{"remember": true, "rememberforever": true, "sear": true, "flexible": true}},
	// Hub::withScope() pushes a scope, calls the callback and pops it; configureScope() calls it
	{"sentry\\state\\hub", map[string]bool{"withscope": true, "configurescope": true}},
}

// isSynchronousCallback reports a call listed in synchronousCallbacks, by the declared type of
// its receiver or by the class the container resolved it to.
func isSynchronousCallback(declared, resolved, method string) bool {
	method = strings.ToLower(method)
	for _, class := range []string{declared, resolved} {
		class = strings.ToLower(normalizeClassName(class))
		if class == "" {
			continue
		}
		for _, entry := range synchronousCallbacks {
			if strings.HasPrefix(class, entry.prefix) && entry.methods[method] {
				return true
			}
		}
	}
	return false
}

// retentionNode is a method to read: method resolved from start, called on an instance of
// receiver, with the closure in the argument at pos. static is true for a static call, which
// needs no instance. fresh is true inside the constructor of an object the caller just created:
// what it stores in its own properties lives no longer than that object.
type retentionNode struct {
	receiver, start, method string
	pos                     int
	static, fresh           bool
	// site is true for the constructor of the call site (new X($closure) in the arguments of the
	// dependency call) and the methods it calls on that object: what they store stays in the
	// object, whose fate the outer call decides.
	site bool
}

// methodLookup is the outcome of looking a method up on a class.
type methodLookup int

const (
	// lookupFound: the declaration was read.
	lookupFound methodLookup = iota
	// lookupAbsent: the class, its traits and its parents were read, and none declares it.
	lookupAbsent
	// lookupUnreadable: a class on the way could not be read, or its traits are adapted.
	lookupUnreadable
)

// resolvedMethod is a method declaration: the class or trait declaring it, and the class it
// belongs to (the class using the trait), from which self:: and parent:: resolve.
type resolvedMethod struct {
	declaring, owner string
	method           analyzer.MethodCallables
}

// closureRetention tells whether the method named by ref keeps the closure it is given, following
// the methods it passes the closure on to. It also returns the class the receiver resolved to.
func (a *Auditor) closureRetention(ref *symbol.ClosureArgRef, callerClass string) (retention, string) {
	start, ok := a.retentionStart(ref, callerClass)
	root := start.receiver
	if !ok {
		return retentionUnknown, root
	}

	queue := []retentionNode{start}
	seen := map[retentionNode]bool{queue[0]: true}
	unknown, held := false, false
	for len(queue) > 0 {
		if len(seen) > maxRetentionNodes {
			return retentionUnknown, root
		}
		node := queue[0]
		queue = queue[1:]

		found, fate, ok := a.paramFate(node)
		if fate.ResultKept != nil && a.resultMayReturn(node, found, fate.ResultFrom) {
			// $this->source = $this->replace($modifiers): what happens to the result counts
			// only if replace() may return the closure
			fate = mergeResultFate(fate)
		}
		if fate.Global || (fate.Retained && !node.fresh) {
			return retentionKept, root
		}
		if fate.Retained && node.site {
			// Stored in the object built at the call site
			held = true
		}
		if !ok || fate.Unknown {
			unknown = true
		}
		for _, f := range fate.Forwards {
			next, ok := a.forwardNode(node, found, f)
			if !ok {
				unknown = true
				continue
			}
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	switch {
	case unknown:
		return retentionUnknown, root
	case held:
		return retentionHeld, root
	}
	return retentionNone, root
}

// retentionStart gives the method to read first for ref. ok is false when it cannot be read; the
// node still names the class the receiver resolved to, when it did.
func (a *Auditor) retentionStart(ref *symbol.ClosureArgRef, callerClass string) (retentionNode, bool) {
	start := retentionNode{method: strings.ToLower(ref.Method), pos: ref.Pos}
	if ref.Class == "" || ref.Method == "" || !ref.Positional {
		return start, false
	}
	if ref.Outer != nil {
		// new X(...): the very class, built for this call
		start.receiver, start.start = normalizeClassName(ref.Class), normalizeClassName(ref.Class)
		start.static, start.fresh, start.site = true, true, true
		return start, true
	}
	root, exact := a.concreteClassFor(ref.Class, callerClass)
	start.receiver, start.start = root, root
	// The property may hold a subclass whose method does something else
	return start, root != "" && (exact || !a.overriddenInSubclass(root, ref.Method))
}

// resultMayReturn reports whether one of the methods the value is passed to may return it: one
// that returns it, or one that cannot be read.
func (a *Auditor) resultMayReturn(node retentionNode, found resolvedMethod, from []analyzer.ParamForward) bool {
	for _, f := range from {
		next, ok := a.forwardNode(node, found, f)
		if !ok {
			return true
		}
		if _, fate, readable := a.paramFate(next); !readable || fate.Returned {
			return true
		}
	}
	return false
}

// mergeResultFate adds to a parameter's fate what the method does with a call result that may
// hold it.
func mergeResultFate(fate analyzer.ParamFate) analyzer.ParamFate {
	result := *fate.ResultKept
	fate.Retained = fate.Retained || result.Retained
	fate.Global = fate.Global || result.Global
	// Passed on again: where it goes from there is not followed
	fate.Unknown = fate.Unknown || result.Unknown || len(result.Forwards) > 0
	return fate
}

// paramFate reads what the method of node does with the argument at its position. ok is false
// when the method cannot be read.
func (a *Auditor) paramFate(node retentionNode) (found resolvedMethod, fate analyzer.ParamFate, ok bool) {
	receiver, readable := a.classCallables(node.receiver)
	if !readable || (!node.static && !receiver.Concrete) {
		return found, fate, false
	}
	found, lookup := a.findCallableMethod(node.start, node.method, map[string]bool{})
	switch {
	case lookup == lookupAbsent && node.method == "__construct":
		// No constructor anywhere: PHP drops the arguments
		return found, fate, true
	case lookup != lookupFound || !found.method.HasBody:
		// Unreadable, abstract, or absent (it may go through __call())
		return found, fate, false
	}
	index := node.pos
	if index >= len(found.method.Params) {
		if found.method.VariadicFrom < 0 {
			// An extra argument: PHP drops it, unless the method reads func_get_args()
			return found, fate, !found.method.Opaque
		}
		index = found.method.VariadicFrom
	}
	fate = found.method.Params[index]
	for _, name := range fate.Trusts {
		if a.declaresFunction(name) {
			// array_map() in that namespace is the project's own function, not PHP's
			fate.Unknown = true
		}
	}
	return found, fate, true
}

// declaresFunction reports a namespaced function declared by an audited file, or by a file
// Composer always loads (the "files" autoload of the project and of its packages).
func (a *Auditor) declaresFunction(name string) bool {
	key := strings.ToLower(normalizeClassName(name))
	a.mu.Lock()
	audited := a.auditedFunctions[key]
	functions := a.userFunctions
	a.mu.Unlock()
	if audited {
		return true
	}
	if a.Symfony == nil || a.Symfony.Root == "" {
		return false
	}
	if functions == nil {
		functions = map[string]bool{}
		for _, path := range composerAutoloadFiles(a.Symfony.Root) {
			content, err := a.getFileContent(path)
			if err != nil {
				continue
			}
			for _, fn := range analyzer.DeclaredFunctions(content) {
				functions[strings.ToLower(normalizeClassName(fn))] = true
			}
		}
		a.mu.Lock()
		a.userFunctions = functions
		a.mu.Unlock()
	}
	return functions[key]
}

// composerAutoloadFiles lists the files of vendor/composer/autoload_files.php.
func composerAutoloadFiles(root string) []string {
	vendor := filepath.Join(root, "vendor")
	data, err := os.ReadFile(filepath.Join(vendor, "composer", "autoload_files.php"))
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range composerPath.FindAllStringSubmatch(string(data), -1) {
		base := root
		if p[1] == "vendorDir" {
			base = vendor
		}
		out = append(out, filepath.Join(base, filepath.FromSlash(phpUnquote(p[2]))))
	}
	return out
}

// forwardNode gives the method a parameter is passed on to.
func (a *Auditor) forwardNode(node retentionNode, found resolvedMethod, f analyzer.ParamForward) (retentionNode, bool) {
	method := strings.ToLower(f.Method)
	switch f.Kind {
	case analyzer.ForwardSelf:
		start := node.receiver
		if f.Self {
			// self:: in a trait is the class using it
			start = found.owner
		}
		return retentionNode{receiver: node.receiver, start: start, method: method, pos: f.Pos, static: node.static, fresh: node.fresh, site: node.site}, true
	case analyzer.ForwardParent:
		// parent:: in a trait is the parent of the class using it
		class, ok := a.classCallables(found.owner)
		if !ok || class.Parent == "" {
			return retentionNode{}, false
		}
		return retentionNode{receiver: node.receiver, start: class.Parent, method: method, pos: f.Pos, static: node.static, fresh: node.fresh, site: node.site}, true
	case analyzer.ForwardDependency:
		class, exact := a.concreteClassFor(f.Class, node.receiver)
		if class == "" || (!exact && a.overriddenInSubclass(class, f.Method)) {
			return retentionNode{}, false
		}
		return retentionNode{receiver: class, start: class, method: method, pos: f.Pos}, true
	case analyzer.ForwardStatic:
		class := normalizeClassName(f.Class)
		if class == "" {
			return retentionNode{}, false
		}
		return retentionNode{receiver: class, start: class, method: method, pos: f.Pos, static: true, fresh: method == "__construct"}, true
	}
	return retentionNode{}, false
}

// concreteClassFor gives the class of the object a property typed typeName holds, as seen from
// the class from. With a container, the services injected into from decide: the one whose class
// is of that type, the default service of the type when it is among them. When they say nothing
// (from is not a service, or the dependency is not among its arguments), the default service of
// the type is used. exact is true when the container names the very class. Without a container,
// the type itself is returned. An empty class means the binding cannot be established.
func (a *Auditor) concreteClassFor(typeName, from string) (class string, exact bool) {
	typeName = normalizeClassName(typeName)
	if typeName == "" {
		return "", false
	}
	if a.Symfony != nil && a.Symfony.Container != nil {
		defaultID, def, hasDefault := a.serviceBehind(typeName)
		switch injected, verdict := a.injectedClass(typeName, from, defaultID); verdict {
		case injectedKnown:
			return injected, true
		case injectedAmbiguous:
			return "", false
		}
		if hasDefault {
			if strings.EqualFold(normalizeClassName(def.Class), normalizeClassName(from)) && def.Decorates != "" {
				// A decorator asking for the type it implements gets the service it decorates
				if _, inner, ok := a.serviceBehind(def.Decorates); ok {
					return normalizeClassName(inner.Class), true
				}
				return "", false
			}
			return normalizeClassName(def.Class), true
		}
	}
	a.mu.Lock()
	target, ok := a.InterfaceAliases[typeName]
	a.mu.Unlock()
	if ok && target != "" {
		return normalizeClassName(target), true
	}
	return typeName, false
}

// injection is what the arguments of a service say about one of its dependencies.
type injection int

const (
	// injectedUnseen: no argument of that type: nothing to conclude.
	injectedUnseen injection = iota
	// injectedKnown: a single class of that type is injected.
	injectedKnown
	// injectedAmbiguous: several classes of that type, or an argument whose type cannot be read.
	injectedAmbiguous
)

// injectedClass looks at the services injected into the class from (constructor arguments and
// method calls of its definitions) and gives the class of the one typed typeName.
func (a *Auditor) injectedClass(typeName, from, defaultID string) (string, injection) {
	from = normalizeClassName(from)
	classes := map[string]string{}
	unsure := false
	for _, caller := range a.Symfony.Container.Definitions {
		if from == "" || caller.IsExcluded() || !strings.EqualFold(normalizeClassName(caller.Class), from) {
			continue
		}
		for _, id := range serviceArguments(caller) {
			resolvedID, def, ok := a.serviceBehind(id)
			if !ok {
				continue
			}
			class := normalizeClassName(def.Class)
			if defaultID != "" && resolvedID == defaultID {
				classes[strings.ToLower(class)] = class
				continue
			}
			switch a.isA(class, typeName, map[string]bool{}) {
			case triYes:
				classes[strings.ToLower(class)] = class
			case triMaybe:
				unsure = true
			}
		}
	}
	switch {
	case len(classes) == 1 && !unsure:
		for _, class := range classes {
			return class, injectedKnown
		}
	case len(classes) == 0 && !unsure:
		return "", injectedUnseen
	}
	return "", injectedAmbiguous
}

// serviceArguments lists the ids of the services a definition is given.
func serviceArguments(def symbol.SymfonyService) []string {
	var ids []string
	for _, arg := range def.Arguments {
		switch value := arg.(type) {
		case map[string]any:
			if kind, _ := value["type"].(string); kind == "service" {
				if id, _ := value["id"].(string); id != "" {
					ids = append(ids, id)
				}
			}
		case string:
			if strings.HasPrefix(value, "@") {
				ids = append(ids, strings.TrimPrefix(value, "@"))
			}
		}
	}
	return ids
}

type tri int

const (
	triNo tri = iota
	triYes
	triMaybe
)

// isA tells whether class is typeName, extends it or implements it. It answers triMaybe when a
// class or interface on the way cannot be read.
func (a *Auditor) isA(class, typeName string, visited map[string]bool) tri {
	class = normalizeClassName(class)
	if strings.EqualFold(class, typeName) {
		return triYes
	}
	key := strings.ToLower(class)
	if class == "" || visited[key] {
		return triNo
	}
	visited[key] = true
	info, ok := a.classCallables(class)
	if !ok {
		if !strings.Contains(class, "\\") {
			// \Countable, \IteratorAggregate, \Stringable: a PHP class has no file, and it is
			// not typeName
			return triNo
		}
		return triMaybe
	}
	result := triNo
	supers := info.Interfaces
	if info.Parent != "" {
		supers = append(append([]string{}, supers...), info.Parent)
	}
	for _, super := range supers {
		switch a.isA(super, typeName, visited) {
		case triYes:
			return triYes
		case triMaybe:
			result = triMaybe
		}
	}
	return result
}

// serviceBehind finds the definition a type hint or a service id resolves to, with its id:
// through the aliases, then by id, then by class when a single service has it.
func (a *Auditor) serviceBehind(name string) (string, symbol.SymfonyService, bool) {
	defs := a.Symfony.Container.Definitions
	ids := a.resolveAliases(name)
	for i := len(ids) - 1; i >= 0; i-- {
		for id, def := range defs {
			if strings.EqualFold(normalizeClassName(id), ids[i]) && def.Class != "" {
				return id, def, true
			}
		}
	}
	matchID, count := "", 0
	for id, def := range defs {
		if def.Class != "" && !def.IsExcluded() && strings.EqualFold(normalizeClassName(def.Class), normalizeClassName(name)) {
			matchID = id
			count++
		}
	}
	if count != 1 {
		return "", symbol.SymfonyService{}, false
	}
	return matchID, defs[matchID], true
}

// overriddenInSubclass reports whether an audited subclass of class declares method.
func (a *Auditor) overriddenInSubclass(class, method string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.overriddenBelow(normalizeClassName(class), strings.ToLower(method))
}

// findCallableMethod finds the declaration of method as PHP resolves it on class: the class
// itself, then its traits, then its parent. It gives up on a class it cannot read, and on a
// class whose trait use adapts methods (insteadof, as).
func (a *Auditor) findCallableMethod(class, method string, visited map[string]bool) (resolvedMethod, methodLookup) {
	key := strings.ToLower(class)
	if class == "" || visited[key] {
		return resolvedMethod{}, lookupAbsent
	}
	visited[key] = true
	info, ok := a.classCallables(class)
	if !ok {
		return resolvedMethod{}, lookupUnreadable
	}
	if m, ok := info.Methods[method]; ok {
		return resolvedMethod{declaring: class, owner: class, method: m}, lookupFound
	}
	if info.TraitRules {
		return resolvedMethod{}, lookupUnreadable
	}
	for _, trait := range info.Traits {
		found, lookup := a.findCallableMethod(trait, method, visited)
		switch lookup {
		case lookupUnreadable:
			// The method may be the trait's: without it nothing can be said
			return resolvedMethod{}, lookupUnreadable
		case lookupFound:
			found.owner = class
			return found, lookupFound
		}
	}
	return a.findCallableMethod(info.Parent, method, visited)
}

// classCallables reads the file declaring class and returns what its methods do with their
// parameters. Vendor classes and safe namespaces are read too: the question is what the method
// does with the closure, not whether the class leaks.
func (a *Auditor) classCallables(class string) (analyzer.ClassCallables, bool) {
	class = normalizeClassName(class)
	path := a.classFile(class)
	if path == "" {
		return analyzer.ClassCallables{}, false
	}
	a.mu.Lock()
	classes, cached := a.callables[path]
	a.mu.Unlock()
	if !cached {
		content, err := a.getFileContent(path)
		if err != nil {
			return analyzer.ClassCallables{}, false
		}
		classes = map[string]analyzer.ClassCallables{}
		for name, info := range analyzer.SummarizeCallables(content) {
			classes[strings.ToLower(normalizeClassName(name))] = info
		}
		a.mu.Lock()
		if a.callables == nil {
			a.callables = make(map[string]map[string]analyzer.ClassCallables)
		}
		a.callables[path] = classes
		a.mu.Unlock()
	}
	info, ok := classes[strings.ToLower(class)]
	return info, ok
}

// classFile locates the file declaring class: among the audited files, in the container's class
// map, then through the project's Composer autoloader.
func (a *Auditor) classFile(class string) string {
	key := strings.ToLower(class)
	a.mu.Lock()
	path, ok := a.classFiles[key]
	a.mu.Unlock()
	if ok {
		return path
	}
	if a.Symfony != nil {
		if p, found := a.Symfony.ClassToFile[class]; found {
			path = p
		} else {
			for name, p := range a.Symfony.ClassToFile {
				if strings.ToLower(normalizeClassName(name)) == key {
					path = p
					break
				}
			}
		}
		if path == "" {
			path = a.composerClassFile(class)
		}
	}
	if path != "" {
		path = filepath.Clean(path)
	}
	a.mu.Lock()
	if a.classFiles == nil {
		a.classFiles = make(map[string]string)
	}
	a.classFiles[key] = path
	a.mu.Unlock()
	return path
}

// composerAutoload is the part of vendor/composer the closure rule reads to locate a class the
// container map does not list (a value object, an interface, a static helper).
type composerAutoload struct {
	// psr4 maps a namespace prefix (with its trailing backslash) to its directories.
	psr4 map[string][]string
	// classmap maps a lowercased class name to its file.
	classmap map[string]string
}

var (
	composerEntry = regexp.MustCompile(`^\s*'((?:[^'\\]|\\.)*)'\s*=>\s*(.*?),?\s*$`)
	composerPath  = regexp.MustCompile(`\$(vendorDir|baseDir)\s*\.\s*'((?:[^'\\]|\\.)*)'`)
)

// loadComposerAutoload reads vendor/composer/autoload_psr4.php and autoload_classmap.php, which
// Composer writes as one array entry per line.
func loadComposerAutoload(root string) *composerAutoload {
	out := &composerAutoload{psr4: map[string][]string{}, classmap: map[string]string{}}
	vendor := filepath.Join(root, "vendor")
	read := func(name string, entry func(key string, paths []string)) {
		data, err := os.ReadFile(filepath.Join(vendor, "composer", name))
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			m := composerEntry.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			var paths []string
			for _, p := range composerPath.FindAllStringSubmatch(m[2], -1) {
				base := root
				if p[1] == "vendorDir" {
					base = vendor
				}
				paths = append(paths, filepath.Join(base, filepath.FromSlash(phpUnquote(p[2]))))
			}
			if len(paths) > 0 {
				entry(phpUnquote(m[1]), paths)
			}
		}
	}
	read("autoload_psr4.php", func(prefix string, paths []string) { out.psr4[prefix] = paths })
	read("autoload_classmap.php", func(class string, paths []string) { out.classmap[strings.ToLower(class)] = paths[0] })
	return out
}

// phpUnquote undoes the escaping of a single-quoted PHP string.
func phpUnquote(s string) string {
	return strings.NewReplacer(`\\`, `\`, `\'`, `'`).Replace(s)
}

// composerClassFile locates class through the project's Composer autoloader: the class map,
// then the longest PSR-4 prefix.
func (a *Auditor) composerClassFile(class string) string {
	if a.Symfony == nil || a.Symfony.Root == "" {
		return ""
	}
	a.mu.Lock()
	if a.composer == nil {
		a.composer = loadComposerAutoload(a.Symfony.Root)
	}
	autoload := a.composer
	a.mu.Unlock()

	if path, ok := autoload.classmap[strings.ToLower(class)]; ok {
		return path
	}
	best := ""
	for prefix := range autoload.psr4 {
		if len(prefix) > len(best) && len(class) > len(prefix) && strings.EqualFold(class[:len(prefix)], prefix) {
			best = prefix
		}
	}
	if best == "" {
		return ""
	}
	relative := filepath.FromSlash(strings.ReplaceAll(class[len(best):], "\\", "/")) + ".php"
	for _, dir := range autoload.psr4[best] {
		path := filepath.Join(dir, relative)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}
