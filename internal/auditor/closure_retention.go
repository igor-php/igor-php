package auditor

import (
	"path/filepath"
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
			verdict, class := a.closureRetention(ref, f.ContextClass)
			if verdict == retentionUnknown && isSynchronousCallback(ref.Class, class, ref.Method) {
				verdict = retentionNone
			}
			switch verdict {
			case retentionNone:
				changed = true
				continue
			case retentionUnknown:
				if !isRegistrationName(ref.Method) && f.Severity == "ERROR" {
					f.Severity = "WARNING"
					f.Remediation = strings.Replace(unreadClosureHint, "%s", describeCall(ref, class), 1)
					changed = true
				}
			}
			kept = append(kept, f)
		}
		results[i].Findings = kept
	}
	return changed
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
// needs no instance.
type retentionNode struct {
	receiver, start, method string
	pos                     int
	static                  bool
}

// closureRetention tells whether the method named by ref keeps the closure it is given, following
// the methods it passes the closure on to. It also returns the class the receiver resolved to.
func (a *Auditor) closureRetention(ref *symbol.ClosureArgRef, callerClass string) (retention, string) {
	if ref.Class == "" || ref.Method == "" || !ref.Positional {
		return retentionUnknown, ""
	}
	root, exact := a.concreteClassFor(ref.Class, callerClass)
	if root == "" {
		return retentionUnknown, ""
	}
	if !exact && a.overriddenInSubclass(root, ref.Method) {
		// The property may hold a subclass whose method does something else
		return retentionUnknown, root
	}

	queue := []retentionNode{{receiver: root, start: root, method: strings.ToLower(ref.Method), pos: ref.Pos}}
	seen := map[retentionNode]bool{queue[0]: true}
	unknown := false
	for len(queue) > 0 {
		if len(seen) > maxRetentionNodes {
			return retentionUnknown, root
		}
		node := queue[0]
		queue = queue[1:]

		declaring, fate, ok := a.paramFate(node)
		if fate.Retained {
			return retentionKept, root
		}
		if !ok || fate.Unknown {
			unknown = true
		}
		for _, f := range fate.Forwards {
			next, ok := a.forwardNode(node, declaring, f)
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
	if unknown {
		return retentionUnknown, root
	}
	return retentionNone, root
}

// paramFate reads what the method of node does with the argument at its position, and names
// the class declaring that method. ok is false when the method cannot be read.
func (a *Auditor) paramFate(node retentionNode) (declaring string, fate analyzer.ParamFate, ok bool) {
	receiver, found := a.classCallables(node.receiver)
	if !found || (!node.static && !receiver.Concrete) {
		return "", fate, false
	}
	declaring, method, found := a.findCallableMethod(node.start, node.method, map[string]bool{})
	if !found || !method.HasBody {
		return "", fate, false
	}
	index := node.pos
	if index >= len(method.Params) {
		if method.VariadicFrom < 0 {
			// An extra argument: PHP drops it, unless the method reads func_get_args()
			return declaring, fate, !method.Opaque
		}
		index = method.VariadicFrom
	}
	return declaring, method.Params[index], true
}

// forwardNode gives the method a parameter is passed on to.
func (a *Auditor) forwardNode(node retentionNode, declaring string, f analyzer.ParamForward) (retentionNode, bool) {
	method := strings.ToLower(f.Method)
	switch f.Kind {
	case analyzer.ForwardSelf:
		start := node.receiver
		if f.Self {
			start = declaring
		}
		return retentionNode{receiver: node.receiver, start: start, method: method, pos: f.Pos, static: node.static}, true
	case analyzer.ForwardParent:
		class, ok := a.classCallables(declaring)
		if !ok || class.Parent == "" {
			return retentionNode{}, false
		}
		return retentionNode{receiver: node.receiver, start: class.Parent, method: method, pos: f.Pos, static: node.static}, true
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
		return retentionNode{receiver: class, start: class, method: method, pos: f.Pos, static: true}, true
	}
	return retentionNode{}, false
}

// concreteClassFor gives the class of the object a property typed typeName holds, as seen from
// the class from. With a container, an interface or a service class resolves to the class of the
// service behind it (exact is true: the container names the very class). A decorator asking for
// the type it implements gets the service it decorates. Without one, the type itself is returned.
func (a *Auditor) concreteClassFor(typeName, from string) (class string, exact bool) {
	typeName = normalizeClassName(typeName)
	if typeName == "" {
		return "", false
	}
	if a.Symfony != nil && a.Symfony.Container != nil {
		if def, ok := a.serviceBehind(typeName); ok {
			if strings.EqualFold(normalizeClassName(def.Class), normalizeClassName(from)) && def.Decorates != "" {
				if inner, ok := a.serviceBehind(def.Decorates); ok {
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

// serviceBehind finds the definition a type hint or a service id resolves to: through the
// aliases, then by id, then by class when a single shared service has it.
func (a *Auditor) serviceBehind(name string) (symbol.SymfonyService, bool) {
	defs := a.Symfony.Container.Definitions
	ids := a.resolveAliases(name)
	for i := len(ids) - 1; i >= 0; i-- {
		for id, def := range defs {
			if strings.EqualFold(normalizeClassName(id), ids[i]) && def.Class != "" {
				return def, true
			}
		}
	}
	var match symbol.SymfonyService
	count := 0
	for _, def := range defs {
		if def.Class != "" && strings.EqualFold(normalizeClassName(def.Class), name) {
			match = def
			count++
		}
	}
	return match, count == 1
}

// overriddenInSubclass reports whether an audited subclass of class declares method.
func (a *Auditor) overriddenInSubclass(class, method string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.overriddenBelow(normalizeClassName(class), strings.ToLower(method))
}

// findCallableMethod finds the declaration of method as PHP resolves it on class: the class
// itself, then its traits, then its parent. It gives up on a class it cannot read.
func (a *Auditor) findCallableMethod(class, method string, visited map[string]bool) (string, analyzer.MethodCallables, bool) {
	key := strings.ToLower(class)
	if class == "" || visited[key] {
		return "", analyzer.MethodCallables{}, false
	}
	visited[key] = true
	info, ok := a.classCallables(class)
	if !ok {
		return "", analyzer.MethodCallables{}, false
	}
	if m, ok := info.Methods[method]; ok {
		return class, m, true
	}
	for _, trait := range info.Traits {
		if _, known := a.classCallables(trait); !known {
			// The method may be the trait's: without it nothing can be said
			return "", analyzer.MethodCallables{}, false
		}
		if declaring, m, ok := a.findCallableMethod(trait, method, visited); ok {
			return declaring, m, true
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

// classFile locates the file declaring class: in the container's class map, among the audited
// files, then through the project's Composer autoloader.
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
