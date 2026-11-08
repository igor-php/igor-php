package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

const (
	defaultBaselineFile = "igor-baseline.json"
	minBaselineReason   = 15
)

// addBaselineTools registers the baseline tools: baseline_check reads, baseline_add and baseline_prune
// write the baseline file of the project, unless the server runs with --read-only.
func (s *mcpServer) addBaselineTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "baseline_check",
		Description: "Checks the baseline of the project (igor-baseline.json): lists its entries that igor no longer " +
			"detects, which baseline_prune removes.",
		Annotations: &mcp.ToolAnnotations{Title: "Check baseline", ReadOnlyHint: true, OpenWorldHint: boolPtr(false)},
	}, s.baselineCheckTool)

	if s.readOnly {
		return
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "baseline_add",
		Description: "Accepts one finding in the baseline of the project, with the reason it is safe. " +
			"Only for a finding the developer agreed is a false positive or safe by design: fix the leak instead whenever you can. " +
			"The reason must explain why the state cannot leak between requests; a placeholder such as TODO is refused. " +
			"The baseline matches findings by message: it accepts every finding with this message in the file.",
		Annotations: &mcp.ToolAnnotations{Title: "Add to baseline", DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}, s.baselineAddTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "baseline_prune",
		Description: "Removes from the baseline of the project the entries igor no longer detects, and rewrites the file.",
		Annotations: &mcp.ToolAnnotations{Title: "Prune baseline", DestructiveHint: boolPtr(true), IdempotentHint: true, OpenWorldHint: boolPtr(false)},
	}, s.baselinePruneTool)
}

// baselineFile returns the path of the baseline of the project, as written in the config or the default one.
func (s *mcpServer) baselineFile() string {
	path := s.cfg.BaselinePath
	if path == "" {
		path = defaultBaselineFile
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.root, path)
	}
	return path
}

// rawAudit audits the project, or one file, without hiding the findings of the baseline, and without the
// baselines of vendor packages, as --check-baseline does. The caller holds s.mu.
func (s *mcpServer) rawAudit(target string) ([]symbol.AuditStatus, config.Baseline, error) {
	project, err := s.loaded(false)
	if err != nil {
		return nil, config.Baseline{}, err
	}
	cfg := s.cfg
	cfg.TargetFile = target
	cfg.CheckBaseline = true
	cfg.BaselinePath = s.baselineFile()
	results, baseline := project.audit(&cfg, nil)
	return results, baseline, nil
}

// loadOwnBaseline reads the baseline file of the project. A missing file is an empty baseline.
func (s *mcpServer) loadOwnBaseline() (config.Baseline, error) {
	baseline, err := config.LoadBaseline(s.baselineFile())
	if errors.Is(err, os.ErrNotExist) {
		return config.Baseline{Files: map[string][]config.BaselineEntry{}}, nil
	}
	if err != nil {
		return config.Baseline{}, fmt.Errorf("cannot read the baseline %s: %w", s.baselineFile(), err)
	}
	if baseline.Files == nil {
		baseline.Files = map[string][]config.BaselineEntry{}
	}
	return baseline, nil
}

type baselineEntryOutput struct {
	File    string `json:"file"`
	Message string `json:"message"`
	Reason  string `json:"reason,omitempty"`
}

func (s *mcpServer) staleEntries() ([]config.StaleBaselineEntry, config.Baseline, error) {
	if _, err := os.Stat(s.baselineFile()); errors.Is(err, os.ErrNotExist) {
		return nil, config.Baseline{}, nil
	}
	baseline, err := s.loadOwnBaseline()
	if err != nil {
		return nil, config.Baseline{}, err
	}
	results, _, err := s.rawAudit("")
	if err != nil {
		return nil, config.Baseline{}, err
	}
	stale := config.IdentifyStaleEntries(baseline, results, s.root)
	sort.Slice(stale, func(i, j int) bool {
		if stale[i].FilePath != stale[j].FilePath {
			return stale[i].FilePath < stale[j].FilePath
		}
		return stale[i].Message < stale[j].Message
	})
	return stale, baseline, nil
}

func countEntries(b config.Baseline) int {
	n := 0
	for _, entries := range b.Files {
		n += len(entries)
	}
	return n
}

func toEntryOutputs(stale []config.StaleBaselineEntry) []baselineEntryOutput {
	out := []baselineEntryOutput{}
	for _, e := range stale {
		out = append(out, baselineEntryOutput{File: filepath.ToSlash(e.FilePath), Message: e.Message})
	}
	return out
}

// --- baseline_check

type baselineCheckOutput struct {
	Baseline string                `json:"baseline"`
	Exists   bool                  `json:"exists"`
	Entries  int                   `json:"entries"`
	Clean    bool                  `json:"clean" jsonschema:"every entry still matches a finding"`
	Stale    []baselineEntryOutput `json:"stale" jsonschema:"entries igor no longer detects"`
}

func (s *mcpServer) baselineCheckTool(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, baselineCheckOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := baselineCheckOutput{Baseline: s.relative(s.baselineFile()), Stale: []baselineEntryOutput{}}
	if _, err := os.Stat(s.baselineFile()); err == nil {
		out.Exists = true
	}
	stale, baseline, err := s.staleEntries()
	if err != nil {
		return nil, out, err
	}
	out.Entries = countEntries(baseline)
	out.Stale = toEntryOutputs(stale)
	out.Clean = len(stale) == 0
	return nil, out, nil
}

// --- baseline_prune

type baselinePruneOutput struct {
	Baseline string                `json:"baseline"`
	Removed  []baselineEntryOutput `json:"removed"`
	Entries  int                   `json:"entries" jsonschema:"entries left in the baseline"`
}

func (s *mcpServer) baselinePruneTool(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, baselinePruneOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := baselinePruneOutput{Baseline: s.relative(s.baselineFile()), Removed: []baselineEntryOutput{}}
	stale, baseline, err := s.staleEntries()
	if err != nil {
		return nil, out, err
	}
	out.Entries = countEntries(baseline)
	if len(stale) == 0 {
		return nil, out, nil
	}
	pruned := config.PruneBaseline(baseline, stale)
	if err := config.WriteBaseline(s.baselineFile(), pruned); err != nil {
		return nil, out, fmt.Errorf("cannot write the baseline %s: %w", s.baselineFile(), err)
	}
	out.Removed = toEntryOutputs(stale)
	out.Entries = countEntries(pruned)
	return nil, out, nil
}

// --- baseline_add

type baselineAddInput struct {
	Path    string `json:"path" jsonschema:"the file of the finding, absolute or relative to the project root"`
	Line    int    `json:"line" jsonschema:"the line of the finding"`
	RuleID  string `json:"rule_id,omitempty" jsonschema:"the rule_id of the finding, needed when several findings share the line"`
	Message string `json:"message,omitempty" jsonschema:"the message of the finding, needed when several findings of the same rule share the line"`
	Reason  string `json:"reason" jsonschema:"why this state cannot leak between requests, e.g. bounded cache keyed by class, reset by kernel.reset"`
}

type baselineAddOutput struct {
	Baseline string              `json:"baseline"`
	Added    baselineEntryOutput `json:"added"`
	Accepts  int                 `json:"accepts" jsonschema:"findings of the file the entry accepts, since the baseline matches by message"`
}

// validateBaselineReason refuses an empty or placeholder reason: a baseline entry must say why it is safe.
func validateBaselineReason(reason string) error {
	r := strings.TrimSpace(reason)
	upper := strings.ToUpper(r)
	for _, placeholder := range []string{"TODO", "FIXME", "TBD", "XXX"} {
		if strings.HasPrefix(upper, placeholder) {
			return fmt.Errorf("the reason %q is a placeholder: explain why this state cannot leak between requests", r)
		}
	}
	if len([]rune(r)) < minBaselineReason {
		return fmt.Errorf("the reason %q is too short: explain why this state cannot leak between requests", r)
	}
	return nil
}

// findingAt returns the finding of results at line, of rule ruleID and with message when they are set.
func findingAt(results []symbol.AuditStatus, line int, ruleID, message string) (symbol.AuditStatus, symbol.Finding, error) {
	var matches []symbol.Finding
	var file symbol.AuditStatus
	for _, res := range results {
		for _, f := range res.Findings {
			if f.Line == line && (ruleID == "" || strings.EqualFold(f.Rule, ruleID)) && (message == "" || f.Message == message) {
				matches = append(matches, f)
				file = res
			}
		}
	}
	switch {
	case len(matches) == 0:
		return file, symbol.Finding{}, fmt.Errorf("no finding at line %d: run audit_file to get the line and rule_id of the findings", line)
	case len(matches) > 1:
		var found []string
		for _, f := range matches {
			found = append(found, fmt.Sprintf("%s: %q", f.Rule, f.Message))
		}
		return file, symbol.Finding{}, fmt.Errorf("%d findings at line %d, pass rule_id or message to pick one: %s", len(matches), line, strings.Join(found, "; "))
	}
	return file, matches[0], nil
}

func (s *mcpServer) baselineAddTool(_ context.Context, _ *mcp.CallToolRequest, in baselineAddInput) (*mcp.CallToolResult, baselineAddOutput, error) {
	if err := validateBaselineReason(in.Reason); err != nil {
		return nil, baselineAddOutput{}, err
	}
	abs, err := s.resolvePath(in.Path)
	if err != nil {
		return nil, baselineAddOutput{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	results, _, err := s.rawAudit(abs)
	if err != nil {
		return nil, baselineAddOutput{}, err
	}
	res, finding, err := findingAt(results, in.Line, in.RuleID, strings.TrimSpace(in.Message))
	if err != nil {
		return nil, baselineAddOutput{}, err
	}

	baseline, err := s.loadOwnBaseline()
	if err != nil {
		return nil, baselineAddOutput{}, err
	}
	key, err := filepath.Rel(s.root, res.FilePath)
	if err != nil {
		key = res.FilePath
	}
	for _, entry := range baseline.Files[key] {
		if entry.Message == finding.Message {
			return nil, baselineAddOutput{}, fmt.Errorf("this finding is already in the baseline, with the reason %q", entry.Reason)
		}
	}
	reason := strings.TrimSpace(in.Reason)
	baseline.Files[key] = append(baseline.Files[key], config.BaselineEntry{Message: finding.Message, Reason: reason})
	if err := config.WriteBaseline(s.baselineFile(), baseline); err != nil {
		return nil, baselineAddOutput{}, fmt.Errorf("cannot write the baseline %s: %w", s.baselineFile(), err)
	}

	out := baselineAddOutput{
		Baseline: s.relative(s.baselineFile()),
		Added:    baselineEntryOutput{File: filepath.ToSlash(key), Message: finding.Message, Reason: reason},
	}
	for _, f := range res.Findings {
		if f.Message == finding.Message {
			out.Accepts++
		}
	}
	return nil, out, nil
}
