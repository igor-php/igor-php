package analyzer

import (
	"fmt"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// staleDependency describes an object that belongs to one request, or to the application clone of one
// request: a persistent service that keeps it keeps the first request's copy for the whole worker life.
type staleDependency struct {
	what string
	hint string
}

var staleDependencies = map[string]staleDependency{
	"Illuminate\\Http\\Request": {
		what: "the HTTP request",
		hint: "Resolve the current request when you need it, with the request() helper, or pass it to the method that uses it.",
	},
	"Symfony\\Component\\HttpFoundation\\Request": {
		what: "the HTTP request",
		hint: "Inject the RequestStack and read getCurrentRequest() when you need it, or pass the request to the method that uses it.",
	},
	"Illuminate\\Contracts\\Container\\Container":    staleContainer,
	"Illuminate\\Container\\Container":               staleContainer,
	"Illuminate\\Contracts\\Foundation\\Application": staleContainer,
	"Illuminate\\Foundation\\Application":            staleContainer,
	"Illuminate\\Contracts\\Config\\Repository":      staleConfig,
	"Illuminate\\Config\\Repository":                 staleConfig,
}

var (
	staleContainer = staleDependency{
		what: "the application container",
		hint: "Resolve the container when you need it, with the app() helper, or inject a closure that returns it.",
	}
	staleConfig = staleDependency{
		what: "the configuration repository",
		hint: "Read the configuration when you need it, with the config() helper, or copy the values you need in the constructor.",
	}
)

// checkStaleDependencies flags the properties of a persistent service typed with a per-request object
// (the request, or the container and config of Octane's per-request application clone).
func (v *PHPVisitor) checkStaleDependencies(classNode *sitter.Node, fullName string) {
	if v.isTransientClass(fullName) {
		return
	}
	body := v.getClassBody(classNode)
	if body == nil {
		return
	}

	method, workerSafeMethod := v.curMethod, v.isWorkerSafeMethod
	defer func() { v.curMethod, v.isWorkerSafeMethod = method, workerSafeMethod }()

	for i := uint(0); i < body.ChildCount(); i++ {
		member := body.Child(i)
		switch member.Kind() {
		case "property_declaration":
			v.curMethod, v.isWorkerSafeMethod = "", false
			v.checkStaleProperty(member, member, "property_element")
		case "method_declaration":
			nameNode := member.ChildByFieldName("name")
			if nameNode == nil || strings.ToLower(v.getContent(nameNode)) != "__construct" {
				continue
			}
			v.curMethod, v.isWorkerSafeMethod = v.getContent(nameNode), v.hasAttribute(member, "WorkerSafe")
			v.checkStalePromotions(member)
		}
	}
}

func (v *PHPVisitor) checkStalePromotions(constructor *sitter.Node) {
	params := constructor.ChildByFieldName("parameters")
	if params == nil {
		return
	}
	for j := uint(0); j < params.ChildCount(); j++ {
		if param := params.Child(j); param.Kind() == "property_promotion_parameter" {
			v.checkStaleProperty(param, param, "variable_name")
		}
	}
}

// checkStaleProperty reports each property declared by n whose type names a stale dependency.
func (v *PHPVisitor) checkStaleProperty(n, typed *sitter.Node, nameKind string) {
	typeNode := typed.ChildByFieldName("type")
	if typeNode == nil {
		return
	}
	dep, typeName, ok := v.staleType(v.getContent(typeNode))
	if !ok {
		return
	}

	for j := uint(0); j < n.ChildCount(); j++ {
		child := n.Child(j)
		if child.Kind() != nameKind {
			continue
		}
		nameNode := child
		if nameKind == "property_element" {
			if nameNode = child.ChildByFieldName("name"); nameNode == nil {
				continue
			}
		}
		prop := strings.TrimPrefix(v.getContent(nameNode), "$")
		if v.workerSafeProps[prop] {
			continue
		}
		msg := fmt.Sprintf("Property '%s' of %s keeps %s (%s) across requests.", prop, v.curClass, dep.what, typeName)
		v.addFinding(n, msg, "This service outlives the request: it keeps the object of the first request. "+dep.hint, "ERROR")
	}
}

// staleType resolves each class of a type declaration (nullable and union types included)
// and returns the first one that is a stale dependency.
func (v *PHPVisitor) staleType(typeStr string) (staleDependency, string, bool) {
	for _, part := range strings.FieldsFunc(typeStr, func(r rune) bool { return r == '|' || r == '&' || r == '(' || r == ')' }) {
		name := strings.TrimPrefix(strings.TrimSpace(part), "?")
		if name == "" || strings.EqualFold(name, "null") {
			continue
		}
		// Class names are case-insensitive in PHP
		fqcn := v.resolveFQCN(name)
		for class, dep := range staleDependencies {
			if strings.EqualFold(class, fqcn) {
				return dep, class, true
			}
		}
	}
	return staleDependency{}, "", false
}
