package auditor

import (
	"sort"
	"strconv"
	"strings"

	"github.com/igor-php/igor-php/internal/analyzer"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// maxStateNodes bounds the methods followed to decide one call: past it the call counts as a mutation.
const maxStateNodes = 200

// RecordClassShape stores what resolving a call on className needs: whether it is abstract and uses traits.
func (a *Auditor) RecordClassShape(className string, shape analyzer.ClassShape) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.classShapes == nil {
		a.classShapes = make(map[string]analyzer.ClassShape)
	}
	a.classShapes[normalizeClassName(className)] = shape
}

// RecordMethodState stores the state summary of a method declared with a body on className.
func (a *Auditor) RecordMethodState(className, methodName string, state analyzer.MethodState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.methodStates == nil {
		a.methodStates = make(map[string]map[string]analyzer.MethodState)
	}
	key := normalizeClassName(className)
	if a.methodStates[key] == nil {
		a.methodStates[key] = make(map[string]analyzer.MethodState)
	}
	a.methodStates[key][strings.ToLower(methodName)] = state
}

// DropStatelessDependencyCalls removes the dependency mutation findings whose called method,
// read in the audited sources, changes neither the dependency's state nor global state. A call
// it cannot resolve (unknown or abstract class, trait or magic method, unaudited parent) keeps
// its finding. It reports whether it removed anything.
func (a *Auditor) DropStatelessDependencyCalls(results []symbol.AuditStatus) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	dropped := false
	for i := range results {
		kept := results[i].Findings[:0]
		for _, f := range results[i].Findings {
			if f.Callee != nil && !a.mayMutate(normalizeClassName(f.Callee.Class), f.Callee.Method, f.Callee.SharedArgs, f.Callee.UnknownArgs) {
				dropped = true
				continue
			}
			kept = append(kept, f)
		}
		results[i].Findings = kept
	}
	return dropped
}

// stateNode is a method to look up: method resolved on start's parent chain, with $this bound to
// receiver. shared lists the argument positions holding state that persists ("*" for any): a
// change to one of those parameters counts as a mutation.
type stateNode struct {
	receiver, start, method string
	shared                  string
	// name is the method as the call spells it: whether it reads as a mutator (removeElement)
	// depends on its case
	name string
}

// sharedArgs is the set of argument positions of a call that hold persistent state.
type sharedArgs struct {
	all       bool
	positions map[int]bool
}

func (s sharedArgs) key() string {
	if s.all {
		return "*"
	}
	keys := make([]int, 0, len(s.positions))
	for p := range s.positions {
		keys = append(keys, p)
	}
	sort.Ints(keys)
	parts := make([]string, len(keys))
	for i, p := range keys {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ",")
}

func parseShared(key string) sharedArgs {
	s := sharedArgs{all: key == "*", positions: map[int]bool{}}
	if s.all || key == "" {
		return s
	}
	for _, part := range strings.Split(key, ",") {
		if p, err := strconv.Atoi(part); err == nil {
			s.positions[p] = true
		}
	}
	return s
}

// hits reports whether the method changes a parameter that receives persistent state.
func (s sharedArgs) hits(state analyzer.MethodState) bool {
	if !state.MutatesArgs {
		return false
	}
	if s.all {
		return true
	}
	for _, param := range state.MutatedParams {
		if s.positions[param] {
			return true
		}
		if state.VariadicFrom >= 0 && param == state.VariadicFrom {
			for p := range s.positions {
				if p >= param {
					return true
				}
			}
		}
	}
	return false
}

// through gives the shared positions of a call made by the method: the arguments holding its own
// state, and those passing on a parameter that received persistent state.
func (s sharedArgs) through(args analyzer.CallArgs) sharedArgs {
	out := sharedArgs{positions: map[int]bool{}}
	for _, p := range args.Shared {
		out.positions[p] = true
	}
	for pos, params := range args.Params {
		for _, param := range params {
			if s.all || s.positions[param] {
				out.positions[pos] = true
			}
		}
	}
	if args.Unknown && (s.all || len(s.positions) > 0 || len(out.positions) > 0) {
		out.all = true
	}
	return out
}

// mayMutate reports whether calling method on an instance of class can change state, following
// the calls on $this, parent:: and on its own dependencies. A change to an argument counts when
// the caller passed its own state there (sharedPositions, or any position with unknownArgs).
func (a *Auditor) mayMutate(class, method string, sharedPositions []int, unknownArgs bool) bool {
	root := sharedArgs{all: unknownArgs, positions: map[int]bool{}}
	for _, p := range sharedPositions {
		root.positions[p] = true
	}
	queue := []stateNode{{class, class, strings.ToLower(method), root.key(), method}}
	seen := map[stateNode]bool{queue[0]: true}
	push := func(n stateNode) {
		if !seen[n] {
			seen[n] = true
			queue = append(queue, n)
		}
	}

	for len(queue) > 0 {
		if len(seen) > maxStateNodes {
			return true
		}
		node := queue[0]
		queue = queue[1:]
		shared := parseShared(node.shared)

		if shape, ok := a.classShapes[node.receiver]; !ok || shape.Abstract || a.overriddenBelow(node.receiver, node.method) {
			// An unaudited class, an interface or an abstract class, or a method a subclass
			// overrides: the instance may be a class whose method does something else
			return true
		}
		declaring, state, ok, stoppedAt := a.resolveMethodState(node.start, node.method)
		if !ok && stoppedAt != "" && a.IsSafeNamespace(stoppedAt) && !analyzer.IsMutatorName(node.name) {
			// $this->getEntityManager(), inherited from a Doctrine repository igor does not audit.
			// An inherited add() (class Cart extends ArrayCollection) still counts: its name decides
			continue
		}
		if !ok || state.Mutates || shared.hits(state) {
			return true
		}
		if !a.pushCalls(node, declaring, state, shared, push) {
			return true
		}
	}
	return false
}

// pushCalls queues the calls a method makes. It reports false when one of them cannot be followed
// and its name suggests a change.
func (a *Auditor) pushCalls(node stateNode, declaring string, state analyzer.MethodState, shared sharedArgs, push func(stateNode)) bool {
	for _, c := range state.SelfCalls {
		start := node.receiver
		if c.Self {
			// self::touch() runs the method as seen from the class declaring the caller
			start = declaring
		}
		push(stateNode{node.receiver, start, strings.ToLower(c.Method), shared.through(c.Args).key(), c.Method})
	}
	for _, c := range state.ParentCalls {
		parent := a.classParents[declaring]
		if parent == "" {
			return false
		}
		push(stateNode{node.receiver, parent, strings.ToLower(c.Method), shared.through(c.Args).key(), c.Method})
	}
	for _, dep := range state.DepCalls {
		c := normalizeClassName(dep.Class)
		if c != "" && a.IsSafeNamespace(c) {
			// $this->connection->insert(): the injected dependency rule does not report a call on
			// a class of a safe namespace, so it does not count one level down either
			continue
		}
		if _, audited := a.classShapes[c]; c == "" || !audited {
			// Not readable: trust the method name
			if dep.Mutator {
				return false
			}
			continue
		}
		push(stateNode{c, c, strings.ToLower(dep.Method), shared.through(dep.Args).key(), dep.Method})
	}
	return true
}

// overriddenBelow reports whether an audited subclass of class declares method, or uses a trait
// that may.
func (a *Auditor) overriddenBelow(class, method string) bool {
	if a.subclasses == nil {
		a.subclasses = make(map[string][]string)
		for child, parent := range a.classParents {
			a.subclasses[parent] = append(a.subclasses[parent], child)
		}
	}
	visited := map[string]bool{class: true}
	stack := append([]string(nil), a.subclasses[class]...)
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[c] {
			continue
		}
		visited[c] = true
		if _, ok := a.methodStates[c][method]; ok || a.classShapes[c].UsesTraits {
			return true
		}
		stack = append(stack, a.subclasses[c]...)
	}
	return false
}

// resolveMethodState finds the declaration of method on class or its parents. It gives up on a
// class it has not audited and on a class using traits before the declaration is found, since a
// trait method would take precedence over the parent's. stoppedAt names the class it has not
// audited, when that is why it gave up and no audited class on the way declares __call(): a call
// it does not find may then be one of the application's own magic methods.
func (a *Auditor) resolveMethodState(class, method string) (declaring string, state analyzer.MethodState, ok bool, stoppedAt string) {
	visited := map[string]bool{}
	magic := false
	for class != "" && !visited[class] {
		visited[class] = true
		shape, audited := a.classShapes[class]
		if !audited {
			if magic {
				// An inherited method wins over __call(), but this one may not exist up there
				return "", analyzer.MethodState{}, false, ""
			}
			return "", analyzer.MethodState{}, false, class
		}
		if state, found := a.methodStates[class][method]; found {
			return class, state, true, ""
		}
		if shape.UsesTraits {
			return "", analyzer.MethodState{}, false, ""
		}
		if _, declared := a.methodStates[class]["__call"]; declared {
			magic = true
		}
		class = a.classParents[class]
	}
	return "", analyzer.MethodState{}, false, ""
}
