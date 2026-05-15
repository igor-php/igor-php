package symbol

import (
	"path/filepath"
	"strings"
)

// Finding represents a single issue detected by the linter.
type Finding struct {
	Message      string
	Code         string
	Snippet      string
	Remediation  string
	Severity     string // "ERROR" or "WARNING"
	Line         int
	ASTDetails   string
	Dependencies []string
}

// Result groups findings by file.
type Result struct {
	FilePath string
	Findings []Finding
}

// SymfonyContainer represents the output of debug:container --format=json.
type SymfonyContainer struct {
	Definitions map[string]SymfonyService `json:"definitions"`
	Aliases     map[string]interface{}    `json:"aliases"`
}

// SymfonyService represents a single service definition in Symfony.
type SymfonyService struct {
	Class     string `json:"class"`
	Public    bool   `json:"public"`
	Shared    bool   `json:"shared"`
	Arguments []any  `json:"arguments"`
}

// AuditStatus represents the audit state of a single service.
type AuditStatus struct {
	ServiceID    string
	FilePath     string
	Status       string // "✅ OK", "❌ KO", "⚠️  WARN", "❓ MISSING"
	Findings     []Finding
	Dependencies []string
	IsShared     bool
	IsPublic     bool
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
