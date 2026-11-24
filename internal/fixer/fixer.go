// Package fixer rewrites PHP sources to repair the findings that can be fixed
// mechanically. It only acts on findings reported by the analyzer, so baselines,
// @igor-ignore comments and #[WorkerSafe] attributes are honored, and it leaves
// a finding untouched whenever it cannot prove the edit is valid.
package fixer

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/igor-php/igor-php/pkg/symbol"
	sitter "github.com/tree-sitter/go-tree-sitter"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// Level selects which fixes are applied.
type Level string

const (
	// LevelSafe only applies fixes that keep the class's intended behavior.
	LevelSafe Level = "safe"
	// LevelRisky also applies fixes that can change runtime behavior.
	LevelRisky Level = "risky"
)

// Rule names reported in changes and skips.
const (
	RuleIncompleteReset = "IncompleteReset"
	RuleStateMutation   = "StateMutation"
	RuleSuperglobals    = "SuperglobalsUsage"
	RuleProcessState    = "ProcessStateMutation"
)

// Options configures a fix run.
type Options struct {
	Level Level
	// HasResetInterface reports whether Symfony's ResetInterface is installed,
	// which the StateMutation fix requires.
	HasResetInterface bool
}

// Change describes one applied fix.
type Change struct {
	Rule        string
	Line        int
	Description string
}

// Skip describes a fixable finding that was left untouched, and why.
type Skip struct {
	Rule   string
	Line   int
	Reason string
}

// Result is the outcome of a fix run on one file.
type Result struct {
	Content []byte
	Applied []Change
	Skipped []Skip
}

var (
	incompleteResetRe = regexp.MustCompile(`^Property '([^']+)' of .+ is mutated but not reset in reset\(\)\.$`)
	stateMutationRe   = regexp.MustCompile(`^Mutation of state '([^']+)' in .+::.*\(\)$`)
	superglobalRe     = regexp.MustCompile(`^Usage of PHP Superglobal (\$_[A-Z]+) is`)
)

const gcDisablePrefix = "Function 'gc_disable'"

type edit struct {
	start, end uint
	text       string
}

type fixer struct {
	content []byte
	root    *sitter.Node
	opts    Options
	edits   []edit
	result  Result
	classes map[string][]*classInfo
	imports map[string]string
	newline string
	// importNames caches how each imported FQCN is referenced once resolved.
	importNames map[string]string
	// injected maps a method's start byte to the Request variable added to it.
	injected map[uint]string
}

// Apply fixes the given findings in content and returns the rewritten source.
func Apply(content []byte, findings []symbol.Finding, opts Options) (Result, error) {
	tree, err := parse(content)
	if err != nil {
		return Result{}, err
	}
	defer tree.Close()

	if tree.RootNode().HasError() {
		return Result{Content: content}, nil
	}

	f := &fixer{
		content:  content,
		root:     tree.RootNode(),
		opts:     opts,
		newline:  detectNewline(content),
		result:   Result{Content: content},
		injected: make(map[uint]string),
	}
	f.classes = f.collectClasses()
	f.imports = f.collectImports()

	ordered := append([]symbol.Finding(nil), findings...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Line != ordered[j].Line {
			return ordered[i].Line < ordered[j].Line
		}
		return ordered[i].Message < ordered[j].Message
	})

	f.fixIncompleteResets(ordered)
	if opts.Level == LevelRisky {
		f.fixStateMutations(ordered)
		f.fixSuperglobals(ordered)
		f.fixGCDisable(ordered)
	}

	if len(f.edits) == 0 {
		return f.result, nil
	}
	output, err := applyEdits(content, f.edits)
	if err != nil {
		return Result{}, err
	}
	check, err := parse(output)
	if err != nil {
		return Result{}, err
	}
	defer check.Close()
	if check.RootNode().HasError() {
		return Result{}, fmt.Errorf("fix would produce invalid PHP")
	}
	f.result.Content = output
	return f.result, nil
}

func parse(content []byte) (*sitter.Tree, error) {
	p := sitter.NewParser()
	defer p.Close()
	if err := p.SetLanguage(sitter.NewLanguage(php.LanguagePHP())); err != nil {
		return nil, fmt.Errorf("failed to set PHP language: %w", err)
	}
	tree := p.Parse(content, nil)
	if tree == nil {
		return nil, fmt.Errorf("failed to parse PHP source")
	}
	return tree, nil
}

func applyEdits(content []byte, edits []edit) ([]byte, error) {
	sorted := append([]edit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })

	var out strings.Builder
	cursor := uint(0)
	for _, e := range sorted {
		if e.start < cursor || e.end < e.start || e.end > uint(len(content)) {
			return nil, fmt.Errorf("conflicting fixes at byte %d", e.start)
		}
		out.Write(content[cursor:e.start])
		out.WriteString(e.text)
		cursor = e.end
	}
	out.Write(content[cursor:])
	return []byte(out.String()), nil
}

func detectNewline(content []byte) string {
	if strings.Contains(string(content), "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func (f *fixer) text(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return string(f.content[n.StartByte():n.EndByte()])
}

func (f *fixer) applied(rule string, line int, description string) {
	f.result.Applied = append(f.result.Applied, Change{Rule: rule, Line: line, Description: description})
}

func (f *fixer) skipped(rule string, line int, reason string) {
	f.result.Skipped = append(f.result.Skipped, Skip{Rule: rule, Line: line, Reason: reason})
}

// lineIndent returns the whitespace at the start of the line containing pos.
func (f *fixer) lineIndent(pos uint) string {
	start := f.lineStart(pos)
	end := start
	for end < uint(len(f.content)) && (f.content[end] == ' ' || f.content[end] == '\t') {
		end++
	}
	return string(f.content[start:end])
}

func (f *fixer) lineStart(pos uint) uint {
	for pos > 0 && f.content[pos-1] != '\n' {
		pos--
	}
	return pos
}

// onlyWhitespaceBefore reports whether pos is the first non-blank byte of its line.
func (f *fixer) onlyWhitespaceBefore(pos uint) bool {
	return strings.TrimSpace(string(f.content[f.lineStart(pos):pos])) == ""
}

func indentUnit(indent string) string {
	if strings.Contains(indent, "\t") {
		return "\t"
	}
	return "    "
}

func walk(n *sitter.Node, visit func(*sitter.Node) bool) {
	if n == nil || !visit(n) {
		return
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		walk(n.Child(i), visit)
	}
}

func sameNode(a, b *sitter.Node) bool {
	return a != nil && b != nil && a.Kind() == b.Kind() && a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte()
}

func contains(outer, inner *sitter.Node) bool {
	return outer != nil && inner != nil && outer.StartByte() <= inner.StartByte() && inner.EndByte() <= outer.EndByte()
}
