package main

import (
        "os"
        "path/filepath"
        "reflect"
        "testing"
)

func TestParseComposerDev(t *testing.T) {
        // Setup a temporary directory for the test
        tempDir, err := os.MkdirTemp("", "composer-test")
        if err != nil {
                t.Fatalf("failed to create temp dir: %v", err)
        }
        defer os.RemoveAll(tempDir)

        composerContent := `{
                "require": {
                        "php": ">=8.4",
                        "symfony/framework-bundle": "8.0.*"
                },
                "require-dev": {
                        "phpunit/phpunit": "^11.0",
                        "symfony/maker-bundle": "^1.50"
                }
        }`

        err = os.WriteFile(filepath.Join(tempDir, "composer.json"), []byte(composerContent), 0644)
        if err != nil {
                t.Fatalf("failed to write composer.json: %v", err)
        }

        expectedDevPackages := []string{
                "phpunit/phpunit",
                "symfony/maker-bundle",
        }

        devPackages, err := ParseComposerDev(tempDir)
        if err != nil {
                t.Fatalf("ParseComposerDev returned error: %v", err)
        }

        if !reflect.DeepEqual(devPackages, expectedDevPackages) {
                t.Errorf("expected %v, got %v", expectedDevPackages, devPackages)
        }

        // Test missing composer.json
        devPackages, err = ParseComposerDev(os.TempDir())
        if err != nil {
                t.Fatalf("ParseComposerDev with missing file returned error: %v", err)
        }
        if len(devPackages) != 0 {
                t.Errorf("expected 0 packages, got %v", len(devPackages))
        }

        // Test malformed composer.json
        err = os.WriteFile(filepath.Join(tempDir, "composer.json"), []byte(`{invalid}`), 0644)
        if err != nil {
                t.Fatalf("failed to write malformed composer.json: %v", err)
        }
        _, err = ParseComposerDev(tempDir)
        if err == nil {
                t.Errorf("expected error for malformed composer.json, got nil")
        }
}
