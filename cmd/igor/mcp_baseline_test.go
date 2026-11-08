package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/internal/config"
)

func readTestBaseline(t *testing.T, root string) config.Baseline {
	t.Helper()
	b, err := config.LoadBaseline(filepath.Join(root, defaultBaselineFile))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMCPBaselineAddRefusals(t *testing.T) {
	root, session := mcpTestProject(t)
	var cart auditFileOutput
	callTool(t, session, "audit_file", map[string]any{"path": "src/Cart.php"}, &cart)
	finding := cart.Findings[0]

	// A placeholder or a missing finding is refused, and nothing is written
	for _, args := range []map[string]any{
		{"path": "src/Cart.php", "line": finding.Line, "reason": "TODO: Explain why this state mutation is safe"},
		{"path": "src/Cart.php", "line": finding.Line, "reason": "safe"},
		{"path": "src/Cart.php", "line": finding.Line + 1, "reason": "Bounded list, cleared by the kernel.reset tag"},
		{"path": "src/Cart.php", "line": finding.Line, "rule_id": "ExecutionTerminator", "reason": "Bounded list, cleared by the kernel.reset tag"},
		{"path": "../Cart.php", "line": finding.Line, "reason": "Bounded list, cleared by the kernel.reset tag"},
	} {
		if msg := callTool(t, session, "baseline_add", args, nil); msg == "" {
			t.Fatalf("baseline_add %v must fail", args)
		}
	}
	if _, err := os.Stat(filepath.Join(root, defaultBaselineFile)); !os.IsNotExist(err) {
		t.Fatalf("a refused baseline_add must not write the baseline: %v", err)
	}

}

func TestMCPBaselineAddHidesTheFinding(t *testing.T) {
	root, session := mcpTestProject(t)

	var cart auditFileOutput
	callTool(t, session, "audit_file", map[string]any{"path": "src/Cart.php"}, &cart)
	finding := cart.Findings[0]

	reason := "Bounded list, cleared by the kernel.reset tag"
	var added baselineAddOutput
	callTool(t, session, "baseline_add", map[string]any{"path": "src/Cart.php", "line": finding.Line, "rule_id": finding.RuleID, "reason": reason}, &added)
	if added.Added.File != "src/Cart.php" || added.Added.Message != finding.Message || added.Added.Reason != reason || added.Accepts != 1 {
		t.Fatalf("added: %+v", added)
	}
	entries := readTestBaseline(t, root).Files[filepath.FromSlash("src/Cart.php")]
	if len(entries) != 1 || entries[0].Message != finding.Message || entries[0].Reason != reason {
		t.Fatalf("baseline file: %+v", entries)
	}

	if msg := callTool(t, session, "baseline_add", map[string]any{"path": "src/Cart.php", "line": finding.Line, "reason": reason}, nil); !strings.Contains(msg, "already in the baseline") {
		t.Fatalf("a second add must be refused, got %q", msg)
	}

	// The audits hide it now
	var after auditFileOutput
	callTool(t, session, "audit_file", map[string]any{"path": "src/Cart.php"}, &after)
	if len(after.Findings) != 0 {
		t.Fatalf("the baselined finding is still reported: %+v", after.Findings)
	}
	var all auditProjectOutput
	callTool(t, session, "audit_project", nil, &all)
	if all.Total != 2 {
		t.Fatalf("expected the 2 other findings, got %+v", all)
	}

	var check baselineCheckOutput
	callTool(t, session, "baseline_check", nil, &check)
	if !check.Exists || check.Entries != 1 || !check.Clean {
		t.Fatalf("check: %+v", check)
	}
}

func TestMCPBaselineCheckAndPrune(t *testing.T) {
	root, session := mcpTestProject(t)

	var check baselineCheckOutput
	callTool(t, session, "baseline_check", nil, &check)
	if check.Exists || check.Entries != 0 || !check.Clean {
		t.Fatalf("no baseline: %+v", check)
	}

	var cart auditFileOutput
	callTool(t, session, "audit_file", map[string]any{"path": "src/Cart.php"}, &cart)
	baseline := config.Baseline{Files: map[string][]config.BaselineEntry{
		filepath.FromSlash("src/Cart.php"):  {{Message: cart.Findings[0].Message, Reason: "kept"}, {Message: "Gone finding", Reason: "fixed since"}},
		filepath.FromSlash("src/Price.php"): {{Message: "Another gone finding", Reason: "fixed since"}},
	}}
	if err := config.WriteBaseline(filepath.Join(root, defaultBaselineFile), baseline); err != nil {
		t.Fatal(err)
	}

	callTool(t, session, "baseline_check", nil, &check)
	if check.Entries != 3 || check.Clean || len(check.Stale) != 2 || check.Stale[0].Message != "Gone finding" || check.Stale[1].File != "src/Price.php" {
		t.Fatalf("check: %+v", check)
	}

	var pruned baselinePruneOutput
	callTool(t, session, "baseline_prune", nil, &pruned)
	if len(pruned.Removed) != 2 || pruned.Entries != 1 {
		t.Fatalf("prune: %+v", pruned)
	}
	left := readTestBaseline(t, root)
	if len(left.Files) != 1 || len(left.Files[filepath.FromSlash("src/Cart.php")]) != 1 {
		t.Fatalf("pruned baseline: %+v", left)
	}

	var again baselinePruneOutput
	callTool(t, session, "baseline_prune", nil, &again)
	if len(again.Removed) != 0 || again.Entries != 1 {
		t.Fatalf("a second prune must change nothing: %+v", again)
	}
}

func TestValidateBaselineReason(t *testing.T) {
	for reason, ok := range map[string]bool{
		"":                               false,
		"ok":                             false,
		"todo: later, really, I promise": false,
		"FIXME this is wrong but quiet":  false,
		"Bounded cache keyed by entity class, at most 40 entries": true,
	} {
		if err := validateBaselineReason(reason); (err == nil) != ok {
			t.Errorf("validateBaselineReason(%q) = %v", reason, err)
		}
	}
}
