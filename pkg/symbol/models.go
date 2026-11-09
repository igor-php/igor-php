package symbol

import (
	"path/filepath"
	"strings"
)

// Finding represents a single issue detected by the linter.
type Finding struct {
	// Rule is the stable identifier of the rule that reported the finding (one of the Rule* constants).
	Rule          string   `json:"rule_id,omitempty"`
	Message       string   `json:"message"`
	Code          string   `json:"code"`
	Snippet       string   `json:"snippet"`
	Remediation   string   `json:"remediation"`
	Severity      string   `json:"severity"` // "ERROR" or "WARNING"
	Line          int      `json:"line"`
	ASTDetails    string   `json:"ast_details"`
	Dependencies  []string `json:"dependencies"`
	ContextClass  string   `json:"context_class,omitempty"`
	ContextMethod string   `json:"context_method,omitempty"`
	// AlsoMethods are the other methods that lead to the finding (an unreset property written in
	// several methods): the finding is reachable as soon as one of them is called
	AlsoMethods  []string `json:"also_methods,omitempty"`
	Reachability string   `json:"reachability,omitempty"`
	Ignored      bool     `json:"ignored,omitempty"`
	IgnoreReason string   `json:"ignore_reason,omitempty"`
	// OutOfRequest names why the code never runs while the worker serves a request
	// (console, profiler, kernel boot...): the CLI folds these findings unless --show-all is set.
	OutOfRequest string `json:"out_of_request,omitempty"`
	// Category groups findings the CLI presents together: CategoryCache or CategoryNotReset.
	Category string `json:"category,omitempty"`
	// Property is the property a cache or not-reset finding is about.
	Property string `json:"property,omitempty"`
	// CacheKey is the key expression of a keyed cache write, e.g. [$format][$type].
	CacheKey string `json:"cache_key,omitempty"`
	// Callee is the method called on an injected dependency for a dependency mutation finding.
	// The auditor drops the finding once it knows that method leaves the dependency's state as is.
	Callee *CallRef `json:"-"`
	// ClosureArg is the call a closure state leak finding is about: the method the closure is
	// passed to. The auditor drops the finding once it knows that method only calls the closure.
	ClosureArg *ClosureArgRef `json:"-"`
}

// ClosureArgRef names the parameter of a method that receives a closure.
type ClosureArgRef struct {
	// Class is the declared type of the receiver, or "" when it is unknown.
	Class  string
	Method string
	// Pos is the position of the argument holding the closure.
	Pos int
	// Positional is false when that argument, or one before it, is named or spread: Pos cannot
	// be trusted.
	Positional bool
}

// CallRef names a method of a class.
type CallRef struct {
	Class  string
	Method string
	// SharedArgs lists the positions of the arguments holding the caller's own state ($this->x,
	// or a local holding it).
	SharedArgs []int
	// UnknownArgs is true when a shared argument is named or spread: any parameter may receive it.
	UnknownArgs bool
}

const (
	// CategoryCache marks a write to a keyed memoization cache ($this->cache[$key] = ...).
	CategoryCache = "cache"
	// CategoryNotReset marks a property of a resettable service that reset() leaves as is.
	CategoryNotReset = "not-reset"
)

// Rule identifiers: stable names of the rules, as listed in the README catalog.
const (
	RuleStateMutation        = "StateMutation"
	RuleSingletonMutation    = "DetectSingletonMutation"
	RuleClosureStateLeak     = "DetectClosureStateLeak"
	RuleStaleDependency      = "StaleDependency"
	RuleIncompleteReset      = "IncompleteReset"
	RuleMissingResetTag      = "MissingResetTag"
	RuleUnsafeStateRestore   = "UnsafeStateRestore"
	RuleDoctrineFlushState   = "DoctrineFlushState"
	RuleProcessStateMutation = "ProcessStateMutation"
	RuleLocalStaticVariable  = "LocalStaticVariable"
	RuleSuperglobalsUsage    = "SuperglobalsUsage"
	RuleExecutionTerminator  = "ExecutionTerminator"
	RuleMagicMethodDestruct  = "MagicMethodDestruct"
)

// Rules lists every rule identifier, in the order of the README catalog.
var Rules = []string{
	RuleSingletonMutation, RuleClosureStateLeak, RuleStateMutation, RuleStaleDependency,
	RuleIncompleteReset, RuleMissingResetTag, RuleUnsafeStateRestore, RuleDoctrineFlushState,
	RuleProcessStateMutation, RuleLocalStaticVariable, RuleSuperglobalsUsage, RuleExecutionTerminator,
	RuleMagicMethodDestruct,
}

// Result groups findings by file.
type Result struct {
	FilePath string    `json:"file_path"`
	Findings []Finding `json:"findings"`
}

// SymfonyContainer represents the output of debug:container --format=json.
type SymfonyContainer struct {
	Definitions map[string]SymfonyService `json:"definitions"`
	Aliases     map[string]interface{}    `json:"aliases"`
	// EntrypointFiles are files run in the worker for every request without a class to map them,
	// such as the files declaring Laravel closure routes.
	EntrypointFiles []string `json:"entrypoint_files,omitempty"`
	// Warnings reported by the exporter, such as services it could not resolve.
	Warnings []string `json:"warnings,omitempty"`
}

// EntrypointTag marks a non-shared service whose code still runs in the worker for every request,
// such as a Laravel controller: it is audited, but its own state is not expected to persist.
const EntrypointTag = "igor.entrypoint"

// SymfonyService represents a single service definition in Symfony.
type SymfonyService struct {
	Class      string `json:"class"`
	Public     bool   `json:"public"`
	Shared     bool   `json:"shared"`
	Arguments  []any  `json:"arguments"`
	Resettable bool   `json:"resettable"`
	Tags       any    `json:"tags"`
	// Decorates is the id of the service this decorator decorates, from its container.decorator tag
	Decorates string `json:"decorates,omitempty"`
}

// HasTag checks if this service has a specific tag in any supported format.
func (s *SymfonyService) HasTag(tagName string) bool {
	if s.Tags == nil {
		return false
	}
	// Try parsing as array/slice of maps (standard format)
	if slice, ok := s.Tags.([]any); ok {
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok {
				if name, ok := m["name"].(string); ok && name == tagName {
					return true
				}
			}
		}
	}
	// Try parsing as map (alternative format)
	if m, ok := s.Tags.(map[string]any); ok {
		if _, exists := m[tagName]; exists {
			return true
		}
	}
	return false
}

// IsResettable checks if this service is resettable in any supported format.
func (s *SymfonyService) IsResettable() bool {
	if s.Resettable {
		return true
	}
	return s.HasTag("kernel.reset")
}

// IsExcluded checks if this service has been explicitly excluded by Symfony (container.excluded tag).
func (s *SymfonyService) IsExcluded() bool {
	return s.HasTag("container.excluded")
}

// AuditStatus represents the audit state of a single service.
type AuditStatus struct {
	ServiceID    string    `json:"service_id"`
	FilePath     string    `json:"file_path"`
	Status       string    `json:"status"` // "✅ OK", "❌ KO", "⚠️  WARN", "❓ MISSING"
	Findings     []Finding `json:"findings"`
	Dependencies []string  `json:"dependencies"`
	IsShared     bool      `json:"is_shared"`
	IsPublic     bool      `json:"is_public"`
	// OutOfRequest is set when every service backed by a class of this file only runs outside a request.
	OutOfRequest string `json:"out_of_request,omitempty"`
}

// IsVendor returns true if the file is part of the vendor directory.
func (a AuditStatus) IsVendor(projectRoot string) bool {
	// Normalize paths to forward slashes to make comparisons platform-independent
	filePath := filepath.ToSlash(a.FilePath)
	rootPath := filepath.ToSlash(projectRoot)

	relPath := filePath
	if rel, found := strings.CutPrefix(filePath, rootPath); found && rel != "" {
		relPath = strings.TrimPrefix(rel, "/")
	}

	if strings.HasPrefix(relPath, "vendor/") {
		return true
	}

	absPath, err := filepath.Abs(a.FilePath)
	if err == nil {
		absPathSlash := filepath.ToSlash(absPath)
		if strings.Contains(absPathSlash, "/vendor/") {
			return true
		}
	}

	return false
}
