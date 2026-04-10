package main

type Finding struct {
	Message     string
	Code        string
	Remediation string
	Severity    string // "ERROR" ou "WARNING"
	Line        int
}

type Result struct {
	FilePath string
	Findings []Finding
}

type Config struct {
	Exclude        []string `json:"exclude"`
	SafeNamespaces []string `json:"safe_namespaces"`
}

// SymfonyContainer represents the output of debug:container --format=json
type SymfonyContainer struct {
	Definitions map[string]SymfonyService `json:"definitions"`
	Aliases     map[string]interface{}    `json:"aliases"`
}

type SymfonyService struct {
	Class  string `json:"class"`
	Public bool   `json:"public"`
	Shared bool   `json:"shared"`
}
