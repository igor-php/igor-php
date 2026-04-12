package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "igor_config_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	t.Run("Default config when file missing", func(t *testing.T) {
		cfg := LoadConfig(tmpDir)
		if len(cfg.Exclude) == 0 {
			t.Error("Expected default excludes")
		}
		if len(cfg.SafeNamespaces) == 0 {
			t.Error("Expected default safe namespaces")
		}
	})

	t.Run("Valid config file", func(t *testing.T) {
		content := `{"exclude": ["custom"], "safe_namespaces": ["My\\"]}`
		err := os.WriteFile(filepath.Join(tmpDir, "igor.json"), []byte(content), 0644)
		if err != nil {
			t.Fatal(err)
		}
		cfg := LoadConfig(tmpDir)
		if cfg.Exclude[0] != "custom" {
			t.Errorf("Expected 'custom' exclude, got %v", cfg.Exclude)
		}
		if cfg.SafeNamespaces[0] != "My\\" {
			t.Errorf("Expected 'My\\' namespace, got %v", cfg.SafeNamespaces)
		}
	})

	t.Run("Corrupted config file", func(t *testing.T) {
		content := `{ invalid json }`
		err := os.WriteFile(filepath.Join(tmpDir, "igor.json"), []byte(content), 0644)
		if err != nil {
			t.Fatal(err)
		}
		cfg := LoadConfig(tmpDir)
		// Should fallback to default values (from struct initialization in LoadConfig)
		if len(cfg.Exclude) == 0 {
			t.Error("Expected default excludes on parse error")
		}
	})
}
