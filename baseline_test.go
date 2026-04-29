package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBaseline(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "igor_baseline_test")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	filePath := filepath.Join(tmpDir, "service.php")
	_ = os.WriteFile(filePath, []byte("<?php class Service {}"), 0644)

	findings := []Finding{
		{Message: "Error 1", Severity: "ERROR"},
		{Message: "Warning 1", Severity: "WARNING"},
	}

	results := []AuditStatus{
		{
			FilePath: filePath,
			Findings: findings,
		},
	}

	baselinePath := filepath.Join(tmpDir, "igor-baseline.json")

	t.Run("Save and Load Baseline", func(t *testing.T) {
		err := SaveBaseline(baselinePath, results, tmpDir)
		if err != nil {
			t.Fatalf("SaveBaseline failed: %v", err)
		}

		baseline, err := LoadBaseline(baselinePath)
		if err != nil {
			t.Fatalf("LoadBaseline failed: %v", err)
		}

		relPath, _ := filepath.Rel(tmpDir, filePath)
		if len(baseline.Files[relPath]) != 2 {
			t.Errorf("Expected 2 findings in baseline for %s, got %d", relPath, len(baseline.Files[relPath]))
		}
	})

	t.Run("Filter Findings", func(t *testing.T) {
		baseline, _ := LoadBaseline(baselinePath)

		// 1. Full match - should result in 0 findings
		filtered := FilterFindings(baseline, filePath, findings, tmpDir)
		if len(filtered) != 0 {
			t.Errorf("Expected 0 findings after filtering, got %d", len(filtered))
		}

		// 2. New finding - should not be filtered
		newFindings := append(findings, Finding{Message: "New Error", Severity: "ERROR"})
		filtered = FilterFindings(baseline, filePath, newFindings, tmpDir)
		if len(filtered) != 1 || filtered[0].Message != "New Error" {
			t.Errorf("Expected 1 new finding, got %v", filtered)
		}

		// 3. Different file - should not be filtered
		otherFile := filepath.Join(tmpDir, "other.php")
		filtered = FilterFindings(baseline, otherFile, findings, tmpDir)
		if len(filtered) != 2 {
			t.Errorf("Expected 2 findings for unbaselined file, got %d", len(filtered))
		}
	})
}
