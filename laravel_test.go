package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLaravel(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "igor_laravel_test")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// 1. Test failure when artisan is missing
	lb, err := DetectLaravel(tmpDir, DefaultConfig())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if lb != nil {
		t.Errorf("Expected nil bridge when artisan is missing")
	}

	// 2. Test success when artisan is present
	_ = os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("<?php"), 0755)
	lb, err = DetectLaravel(tmpDir, DefaultConfig())
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if lb == nil {
		t.Errorf("Expected Laravel bridge to be detected")
	}
	if lb.GetName() != "Laravel" {
		t.Errorf("Expected bridge name 'Laravel', got %s", lb.GetName())
	}
}

func TestLaravelDefaultPaths(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "igor_paths_test")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	_ = os.WriteFile(filepath.Join(tmpDir, "artisan"), []byte("<?php"), 0755)
	config := DefaultConfig()
	auditor := NewAuditor(config)
	
	bridge, _ := DetectLaravel(tmpDir, config)
	auditor.Framework = bridge

	// Since app/ doesn't exist, it should be empty but the logic should have targeted app/
	// We can't easily check internal local variables of collectFiles, 
	// but we can check if it attempted to scan app/ by creating it.
	
	_ = os.MkdirAll(filepath.Join(tmpDir, "app"), 0755)
	_ = os.WriteFile(filepath.Join(tmpDir, "app", "Service.php"), []byte("<?php class Service {}"), 0644)
	
	auditList := collectFiles(tmpDir, config, auditor)
	found := false
	for _, item := range auditList {
		if filepath.Base(item.FilePath) == "Service.php" {
			found = true
			break
		}
	}
	
	if !found {
		t.Errorf("Expected collectFiles to automatically find Service.php in app/ for Laravel")
	}
}
