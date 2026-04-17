package main

import (
	"path/filepath"
	"strings"
)

// Finding represents a single issue detected by the linter.
type Finding struct {
	Message     string
	Code        string
	Remediation string
	Severity    string // "ERROR" or "WARNING"
	Line        int
}

// Result groups findings by file.
type Result struct {
	FilePath string
	Findings []Finding
}

// Config stores linter settings.
type Config struct {
        Exclude        []string `json:"exclude"`
        SafeNamespaces []string `json:"safe_namespaces"`
        ScanVendors    []string `json:"scan_vendors"` // New: paths in vendor/ to scan recursively
        ConsolePath    string   `json:"console_path"`
        Env            string   `json:"env"`
        Verbose        bool     `json:"verbose"`
        NoAgent        bool     `json:"-"` // Skip Igor Agent even if available
        ProdPackages   []string `json:"-"` // List of require packages from composer.json
        DevPackages    []string `json:"-"` // List of require-dev packages from composer.json
}
// SymfonyContainer represents the output of debug:container --format=json.
type SymfonyContainer struct {
	Definitions map[string]SymfonyService `json:"definitions"`
	Aliases     map[string]interface{}    `json:"aliases"`
}

// SymfonyService represents a single service definition in Symfony.
type SymfonyService struct {
	Class  string `json:"class"`
	Public bool   `json:"public"`
	Shared bool   `json:"shared"`
}

// AuditStatus represents the audit state of a single service.
type AuditStatus struct {
	ServiceID string
	FilePath  string
	Status    string // "✅ OK", "❌ KO", "⚠️  WARN", "❓ MISSING"
	Findings  []Finding
}

// IsVendor returns true if the file is part of the vendor directory.
func (a AuditStatus) IsVendor(projectRoot string) bool {
	relPath := a.FilePath
	if rel, found := strings.CutPrefix(a.FilePath, projectRoot); found && rel != "" {
		relPath = strings.TrimPrefix(rel, "/")
	}

	if strings.HasPrefix(relPath, "vendor/") {
		return true
	}

	absPath, _ := filepath.Abs(a.FilePath)
	return strings.Contains(absPath, "/vendor/")
}
