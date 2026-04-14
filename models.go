package main

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
	ConsolePath    string   `json:"console_path"`
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
