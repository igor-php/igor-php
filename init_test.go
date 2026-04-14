package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitConfig(t *testing.T) {
	t.Run("Initialize Generic PHP project", func(t *testing.T) {
		tmpDir, _ := os.MkdirTemp("", "igor_init_generic")
		defer func() { _ = os.RemoveAll(tmpDir) }()

		err := InitConfig(tmpDir)
		if err != nil {
			t.Fatalf("InitConfig failed: %v", err)
		}

		configPath := filepath.Join(tmpDir, "igor.json")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Fatal("igor.json was not created")
		}

		// Verify content
		data, _ := os.ReadFile(configPath)
		var config Config
		_ = json.Unmarshal(data, &config)

		if config.ConsolePath != "bin/console" {
			t.Errorf("Expected default console_path 'bin/console', got %s", config.ConsolePath)
		}
	})

	t.Run("Fail if igor.json already exists", func(t *testing.T) {
		tmpDir, _ := os.MkdirTemp("", "igor_init_exists")
		defer func() { _ = os.RemoveAll(tmpDir) }()

		configPath := filepath.Join(tmpDir, "igor.json")
		_ = os.WriteFile(configPath, []byte("{}"), 0644)

		err := InitConfig(tmpDir)
		if err == nil {
			t.Fatal("InitConfig should fail if igor.json already exists")
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("Expected 'already exists' error, got: %v", err)
		}
	})
}
