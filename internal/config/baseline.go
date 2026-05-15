package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/igor-php/igor-php/pkg/symbol"
)

// LoadBaseline loads the baseline configuration from a file.
func LoadBaseline(path string) (Baseline, error) {
	var b Baseline
	data, err := os.ReadFile(path)
	if err != nil {
		return b, err
	}
	err = json.Unmarshal(data, &b)
	return b, err
}

// SaveBaseline generates a baseline file from the audit results.
func SaveBaseline(path string, results []symbol.AuditStatus, rootPath string) error {
	b := Baseline{
		Files: make(map[string][]BaselineEntry),
	}

	for _, res := range results {
		if len(res.Findings) == 0 {
			continue
		}

		relPath, err := filepath.Rel(rootPath, res.FilePath)
		if err != nil {
			relPath = res.FilePath
		}

		entries := []BaselineEntry{}
		for _, f := range res.Findings {
			entries = append(entries, BaselineEntry{Message: f.Message})
		}
		b.Files[relPath] = entries
	}

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// FilterFindings removes findings that are present in the baseline.
func FilterFindings(baseline Baseline, filePath string, findings []symbol.Finding, rootPath string) []symbol.Finding {
	if baseline.Files == nil {
		return findings
	}

	relPath, err := filepath.Rel(rootPath, filePath)
	if err != nil {
		relPath = filePath
	}

	ignoredEntries, found := baseline.Files[relPath]
	if !found {
		return findings
	}

	filtered := []symbol.Finding{}
	for _, f := range findings {
		isIgnored := false
		for _, entry := range ignoredEntries {
			if entry.Message == f.Message {
				isIgnored = true
				break
			}
		}
		if !isIgnored {
			filtered = append(filtered, f)
		}
	}

	return filtered
}
