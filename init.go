package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InitConfig detects the project type and generates a default igor.json file.
func InitConfig(root string) error {
	configPath := filepath.Join(root, "igor.json")

	// Check if already exists
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("igor.json already exists at %s", configPath)
	}

	// Minimal base configuration
	config := Config{
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
			config.SafeNamespaces = append(config.SafeNamespaces, "Symfony\\", "Doctrine\\")
			config.ConsolePath = "bin/console"
		} else if strings.Contains(content, "laravel/framework") {
			projectType = "Laravel"
			config.SafeNamespaces = append(config.SafeNamespaces, "Illuminate\\", "Laravel\\")
			config.ConsolePath = "artisan"
		}
	}

	// 2. Additional folder detection
	if projectType == "Generic PHP" {
		if _, err := os.Stat(filepath.Join(root, "bin/console")); err == nil {
			projectType = "Symfony (detected via bin/console)"
			config.SafeNamespaces = uniqueStrings(append(config.SafeNamespaces, "Symfony\\", "Doctrine\\"))
			config.ConsolePath = "bin/console"
		} else if _, err := os.Stat(filepath.Join(root, "artisan")); err == nil {
			projectType = "Laravel (detected via artisan)"
			config.SafeNamespaces = uniqueStrings(append(config.SafeNamespaces, "Illuminate\\", "Laravel\\"))
			config.ConsolePath = "artisan"
		}
	}

	// Deduplicate
	config.SafeNamespaces = uniqueStrings(config.SafeNamespaces)
	config.Exclude = uniqueStrings(config.Exclude)

	// 3. Write file
	file, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(configPath, file, 0644); err != nil {
		return err
	}

	fmt.Printf("✨ Igor has successfully initialized your project!\n")
	fmt.Printf("📂 Detected project type: %s\n", projectType)
	fmt.Printf("📝 Configuration saved to: %s\n", configPath)
	fmt.Println("👉 You can now customize 'igor.json' to fit your needs.")

	return nil
}

func uniqueStrings(input []string) []string {
	unique := make(map[string]bool)
	result := []string{}
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
