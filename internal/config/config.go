package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// DefaultConfig returns the standard linter configuration.
func DefaultConfig() Config {
	return Config{
		Exclude: []string{},
		SafeNamespaces: []string{
			"Symfony\\",
			"Doctrine\\",
			"IgorPhp\\IgorBundle\\",
		},
		ConsolePath: "bin/console",
		Env:         "dev",
		Verbose:     false,
	}
}

// LoadConfig loads the configuration from a file (defaulting to igor.json) in the given root directory.
func LoadConfig(root string, customConfigPath string) Config {
	c := DefaultConfig()

	// Auto-detect packages from composer.json
	if prod, dev, err := ParseComposer(root); err == nil {
		c.ProdPackages = prod
		c.DevPackages = dev
	}

	configPath := customConfigPath
	if configPath == "" {
		configPath = filepath.Join(root, "igor.json")
	}

	data, err := os.ReadFile(configPath)
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
	if userConfig.BaselinePath != "" {
		c.BaselinePath = userConfig.BaselinePath
	}

	return c
}

// IsExcluded returns true if the given path matches any of the excluded patterns.
func (c Config) IsExcluded(path string, rootPath string) bool {
	rel, err := filepath.Rel(rootPath, path)
	if err != nil {
		return false
	}
	for _, ex := range c.Exclude {
		// Normalize exclusion pattern by removing trailing slashes
		ex = strings.TrimRight(ex, "/\\")
		if rel == ex || strings.HasPrefix(rel, ex+string(os.PathSeparator)) || strings.HasPrefix(rel, ex+"/") {
			return true
		}
	}
	return false
}
