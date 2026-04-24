package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFullLabsAudit runs a complete audit on both Symfony and Laravel laboratories
// and verifies that the number of findings matches our expectations.
func TestFullLabsAudit(t *testing.T) {
	// Skip if composer vendor is missing (labs not setup)
	if _, err := os.Stat("./examples/demo-leak-symfony/vendor"); err != nil {
		t.Skip("Symfony lab not setup, skipping integration test")
	}
	if _, err := os.Stat("./examples/demo-leak-laravel/vendor"); err != nil {
		t.Skip("Laravel lab not setup, skipping integration test")
	}

	t.Run("Full Audit: Symfony Lab", func(t *testing.T) {
		root, _ := filepath.Abs("./examples/demo-leak-symfony")
		config := LoadConfig(root)
		auditor := NewAuditor(config)

		// Setup Bridge
		bridge, err := DetectFramework(root, config)
		if err != nil {
			t.Fatalf("Failed to detect Symfony framework: %v", err)
		}
		auditor.Framework = bridge

		// Audit
		auditList := collectFiles(root, config, auditor)
		results := []AuditStatus{}
		for _, job := range auditList {
			findings, _ := auditor.Audit(job.FilePath)
			job.Findings = findings
			results = append(results, job)
		}

		// Count KOs and WARNs
		kos := 0
		warns := 0
		for _, res := range results {
			for _, f := range res.Findings {
				if f.Severity == "ERROR" {
					kos++
				} else if f.Severity == "WARNING" {
					warns++
				}
			}
		}

		// Expectations based on current lab state
		if kos < 3 {
			t.Errorf("Expected at least 3 KOs in Symfony Lab, got %d", kos)
		}
		if warns < 1 {
			t.Errorf("Expected at least 1 WARN in Symfony Lab, got %d", warns)
		}
	})

	t.Run("Full Audit: Laravel Lab", func(t *testing.T) {
		root, _ := filepath.Abs("./examples/demo-leak-laravel")
		config := LoadConfig(root)
		auditor := NewAuditor(config)

		// Setup Bridge
		bridge, err := DetectFramework(root, config)
		if err != nil {
			t.Fatalf("Failed to detect Laravel framework: %v", err)
		}
		auditor.Framework = bridge

		// Audit
		auditList := collectFiles(root, config, auditor)
		results := []AuditStatus{}
		for _, job := range auditList {
			findings, _ := auditor.Audit(job.FilePath)
			job.Findings = findings
			results = append(results, job)
		}

		// Count KOs
		kos := 0
		for _, res := range results {
			for _, f := range res.Findings {
				if f.Severity == "ERROR" {
					kos++
				}
			}
		}

		// Expectations based on current lab state
		if kos < 4 {
			t.Errorf("Expected at least 4 KOs in Laravel Lab, got %d", kos)
		}
	})
}
