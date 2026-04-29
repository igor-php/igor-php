package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDemoLeakFeatures(t *testing.T) {
	root, _ := filepath.Abs("./examples/demo-leak-symfony")

	// Ensure we are in the right place
	if _, err := os.Stat(filepath.Join(root, "composer.json")); err != nil {
		t.Skip("demo-leak-symfony example not found, skipping integration test")
	}

	t.Run("Smart Filtering detection in demo-leak-symfony", func(t *testing.T) {
		// 1. Mock require-dev in composer.json
		originalContent, err := os.ReadFile(filepath.Join(root, "composer.json"))
		if err != nil {
			t.Fatalf("failed to read composer.json: %v", err)
		}
		defer func() {
			_ = os.WriteFile(filepath.Join(root, "composer.json"), originalContent, 0644)
		}()

		mockComposer := `{
			"require": {"php": ">=8.4"},
			"require-dev": {"phpunit/phpunit": "^11.0"}
		}`
		if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(mockComposer), 0644); err != nil {
			t.Fatalf("failed to mock composer.json: %v", err)
		}

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

	t.Run("Agent Detection in demo-leak-symfony", func(t *testing.T) {
		// 1. Mock Agent Map
		cacheDir := filepath.Join(root, "var", "cache", "dev")
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			t.Fatalf("failed to create cache dir: %v", err)
		}
		mapPath := filepath.Join(cacheDir, "igor_service_map.json")

		mockMap := `{"definitions": {"test.service": {"class": "App\\Service\\StatefulService", "shared": true}}, "aliases": {}}`
		if err := os.WriteFile(mapPath, []byte(mockMap), 0644); err != nil {
			t.Fatalf("failed to write agent map: %v", err)
		}
		defer func() { _ = os.Remove(mapPath) }()

		// 2. Mock vendor/autoload.php (needed for reflection even with agent)
		vendorDir := filepath.Join(root, "vendor")
		if err := os.MkdirAll(vendorDir, 0755); err != nil {
			t.Fatalf("failed to create vendor dir: %v", err)
		}
		autoloadPath := filepath.Join(vendorDir, "autoload.php")
		// Correct path for StatefulService relative to demo-leak-symfony root is src/Service/StatefulService.php
		autoloadContent := `<?php
spl_autoload_register(function ($class) {
    if ($class === 'App\\Service\\StatefulService') {
        require_once __DIR__ . '/../src/Service/StatefulService.php';
    }
});`
		if err := os.WriteFile(autoloadPath, []byte(autoloadContent), 0644); err != nil {
			t.Fatalf("failed to write mock autoloader: %v", err)
		}
		defer func() { _ = os.Remove(autoloadPath) }()

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
