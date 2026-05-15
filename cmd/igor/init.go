package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/igor-php/igor-php/internal/config"
)

func InitConfig(root string, customConfigPath string) error {
	configPath := customConfigPath
	if configPath == "" {
		configPath = filepath.Join(root, "igor.json")
	}

	// Check if already exists
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("configuration file already exists at %s", configPath)
	}

	// Minimal base configuration
	cfg := config.Config{
		Exclude:        []string{},
		SafeNamespaces: []string{},
		ConsolePath:    "bin/console",
		Env:            "prod",
		Verbose:        false,
	}
	projectType := "Generic PHP"

	// 1. Detect Frameworks via composer.json
	composerPath := filepath.Join(root, "composer.json")
	if data, err := os.ReadFile(composerPath); err == nil {
		content := string(data)
		if strings.Contains(content, "symfony/framework-bundle") {
			projectType = "Symfony"
			cfg.SafeNamespaces = append(cfg.SafeNamespaces, "Symfony\\", "Doctrine\\")
		}
	}

	// 2. Additional folder detection
	if _, err := os.Stat(filepath.Join(root, "bin/console")); err == nil && projectType == "Generic PHP" {
		projectType = "Symfony (detected via bin/console)"
		cfg.SafeNamespaces = append(cfg.SafeNamespaces, "Symfony\\", "Doctrine\\")
	}

	// Deduplicate
	cfg.SafeNamespaces = uniqueStrings(cfg.SafeNamespaces)
	cfg.Exclude = uniqueStrings(cfg.Exclude)

	// 3. Write file
	file, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(configPath, file, 0644); err != nil {
		return err
	}

	fmt.Printf("✨ Igor has successfully initialized your project!\n")
	fmt.Printf("📂 Detected project type: %s\n", projectType)
	fmt.Printf("📝 Configuration saved to: %s\n", configPath)
	fmt.Printf("👉 You can now customize the configuration to fit your needs.\n")

	return nil
}

func uniqueStrings(input []string) []string {
	unique := make(map[string]bool)
	var result []string
	for _, s := range input {
		if s == "" {
			continue
		}
		if !unique[s] {
			unique[s] = true
			result = append(result, s)
		}
	}
	return result
}
