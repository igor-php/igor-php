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
	Exclude []string `json:"exclude"`
}
