package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDemoLeakFeatures(t *testing.T) {
	root, _ := filepath.Abs("./examples/demo-leak")

	// Ensure we are in the right place
	if _, err := os.Stat(filepath.Join(root, "composer.json")); err != nil {
		t.Skip("demo-leak example not found, skipping integration test")
	}

	t.Run("Smart Filtering detection in demo-leak", func(t *testing.T) {
		// 1. Mock require-dev in composer.json
		originalContent, _ := os.ReadFile(filepath.Join(root, "composer.json"))
		defer os.WriteFile(filepath.Join(root, "composer.json"), originalContent, 0644)

		mockComposer := `{
			"require": {"php": ">=8.4"},
			"require-dev": {"phpunit/phpunit": "^11.0"}
		}`
		os.WriteFile(filepath.Join(root, "composer.json"), []byte(mockComposer), 0644)

		// 2. Load config
		cfg := LoadConfig(root)

		found := false
		for _, pkg := range cfg.DevPackages {
			if pkg == "phpunit/phpunit" {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Expected phpunit/phpunit to be detected in dev packages")
		}
	})

	t.Run("Agent Detection in demo-leak", func(t *testing.T) {
		// 1. Mock Agent Map
		cacheDir := filepath.Join(root, "var", "cache", "dev")
		os.MkdirAll(cacheDir, 0755)
		mapPath := filepath.Join(cacheDir, "igor_service_map.json")

		mockMap := `{"definitions": {"test.service": {"class": "App\\Service\\StatefulService", "shared": true}}, "aliases": {}}`
		os.WriteFile(mapPath, []byte(mockMap), 0644)
		defer os.Remove(mapPath)

		// 2. Mock vendor/autoload.php (needed for reflection even with agent)
		vendorDir := filepath.Join(root, "vendor")
		os.MkdirAll(vendorDir, 0755)
		autoloadPath := filepath.Join(vendorDir, "autoload.php")
		// Correct path for StatefulService relative to demo-leak root is src/Service/StatefulService.php
		autoloadContent := `<?php
spl_autoload_register(function ($class) {
    if ($class === 'App\\Service\\StatefulService') {
        require_once __DIR__ . '/../src/Service/StatefulService.php';
    }
});`
		os.WriteFile(autoloadPath, []byte(autoloadContent), 0644)
		defer os.Remove(autoloadPath)

		cfg := DefaultConfig()
		cfg.Env = "dev"

		bridge := NewSymfonyBridge(root, "bin/console", cfg)

		err := bridge.LoadContainer("dev")
		if err != nil {
			t.Fatalf("Failed to load container: %v", err)
		}

		if _, found := bridge.Container.Definitions["test.service"]; !found {
			t.Error("Expected Agent service map to be used")
		}
	})
}
