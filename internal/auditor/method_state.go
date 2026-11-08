package auditor

import (
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
			if f.Callee != nil && !a.mayMutate(normalizeClassName(f.Callee.Class), f.Callee.Method, f.Callee.SharedArgs) {
				dropped = true
				continue
			}
			kept = append(kept, f)
		}
		results[i].Findings = kept
	}
	return dropped
}

// stateNode is a method to look up: method resolved on start's parent chain, with $this bound to receiver.
type stateNode struct {
	receiver, start, method string
}

// mayMutate reports whether calling method on an instance of class can change state, following
// the calls on $this, parent:: and on its own dependencies. With sharedArgs, a change to an
// argument counts too: the caller handed over its own state.
func (a *Auditor) mayMutate(class, method string, sharedArgs bool) bool {
	queue := []stateNode{{class, class, strings.ToLower(method)}}
	seen := map[stateNode]bool{queue[0]: true}

	for len(queue) > 0 {
		if len(seen) > maxStateNodes {
			return true
		}
		node := queue[0]
		queue = queue[1:]

		if shape, ok := a.classShapes[node.receiver]; !ok || shape.Abstract {
			// An unaudited class, an interface or an abstract class: the instance may be any subclass
			return true
		}
		declaring, state, ok := a.resolveMethodState(node.start, node.method)
		if !ok || state.Mutates || (sharedArgs && state.MutatesArgs) {
			return true
		}

		push := func(n stateNode) {
			if !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
		for _, m := range state.SelfCalls {
			push(stateNode{node.receiver, node.receiver, strings.ToLower(m)})
		}
		for _, m := range state.ParentCalls {
			parent := a.classParents[declaring]
			if parent == "" {
				return true
			}
			push(stateNode{node.receiver, parent, strings.ToLower(m)})
		}
		for _, dep := range state.DepCalls {
			c := normalizeClassName(dep.Class)
			if _, audited := a.classShapes[c]; c == "" || !audited {
				// Not readable: trust the method name
				if dep.Mutator {
					return true
				}
				continue
			}
			push(stateNode{c, c, strings.ToLower(dep.Method)})
		}
	}
	return false
}

// resolveMethodState finds the declaration of method on class or its parents. It gives up on a
// class it has not audited and on a class using traits before the declaration is found, since a
// trait method would take precedence over the parent's.
func (a *Auditor) resolveMethodState(class, method string) (string, analyzer.MethodState, bool) {
	visited := map[string]bool{}
	for class != "" && !visited[class] {
		visited[class] = true
		shape, ok := a.classShapes[class]
		if !ok {
			return "", analyzer.MethodState{}, false
		}
		if state, ok := a.methodStates[class][method]; ok {
			return class, state, true
		}
		if shape.UsesTraits {
			return "", analyzer.MethodState{}, false
		}
		class = a.classParents[class]
	}
	return "", analyzer.MethodState{}, false
}
