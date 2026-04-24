package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFullLabsAudit runs a static analysis on both Symfony and Laravel laboratory files
// and verifies that the number of findings matches our expectations.
func TestFullLabsAudit(t *testing.T) {
	t.Run("Static Analysis: Symfony Lab Files", func(t *testing.T) {
		root, _ := filepath.Abs("./examples/demo-leak-symfony")
		if _, err := os.Stat(root); err != nil {
			t.Skip("Symfony lab not found")
		}

		config := LoadConfig(root)
		auditor := NewAuditor(config)
		
		// Use a generic bridge for static-only integration testing
		// this avoids booting Symfony/Laravel during tests
		auditor.Framework = nil 

		auditList := collectLocalFiles(filepath.Join(root, "src"), root, config, auditor, make(map[string]bool))
		
		kos := 0
		warns := 0
		for _, job := range auditList {
			findings, _ := auditor.Audit(job.FilePath)
			for _, f := range findings {
				if f.Severity == "ERROR" {
					kos++
				} else if f.Severity == "WARNING" {
					warns++
				}
			}
		}

		// Expectations for Symfony src/
		if kos < 3 {
			t.Errorf("Expected at least 3 KOs in Symfony Lab files, got %d", kos)
		}
	})

	t.Run("Static Analysis: Laravel Lab Files", func(t *testing.T) {
		root, _ := filepath.Abs("./examples/demo-leak-laravel")
		if _, err := os.Stat(root); err != nil {
			t.Skip("Laravel lab not found")
		}

		config := LoadConfig(root)
		auditor := NewAuditor(config)
		auditor.Framework = nil

		auditList := collectLocalFiles(filepath.Join(root, "app"), root, config, auditor, make(map[string]bool))
		
		kos := 0
		for _, job := range auditList {
			findings, _ := auditor.Audit(job.FilePath)
			for _, f := range findings {
				if f.Severity == "ERROR" {
					kos++
				}
			}
		}

		// Expectations for Laravel app/
		if kos < 3 {
			t.Errorf("Expected at least 3 KOs in Laravel Lab files, got %d", kos)
		}
	})
}
