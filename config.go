package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultConfig returns the standard linter configuration.
func DefaultConfig() Config {
	return Config{
		Exclude: []string{"vendor", "var", "cache", "tests", "Entity", "Dto", "ApiResource"},
		SafeNamespaces: []string{
			"Symfony\\",
			"Doctrine\\",
		},
		ConsolePath: "bin/console",
	}
}

// LoadConfig loads the configuration from igor.json in the given root directory.
func LoadConfig(root string) Config {
	c := DefaultConfig()
	data, err := os.ReadFile(filepath.Join(root, "igor.json"))
	if err != nil {
		return c
	}

	var userConfig Config
	if err := json.Unmarshal(data, &userConfig); err != nil {
		// Log or handle error if needed, for now fallback to defaults
		return c
	}

	// Merge logic (optional): here we just override if provided
	if len(userConfig.Exclude) > 0 {
		c.Exclude = userConfig.Exclude
	}
	if len(userConfig.SafeNamespaces) > 0 {
		c.SafeNamespaces = userConfig.SafeNamespaces
	}

	return c
}
