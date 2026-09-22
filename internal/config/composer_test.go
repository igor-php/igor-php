package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseComposer_MissingFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composer_missing_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	prod, dev, err := ParseComposer(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(prod) != 0 || len(dev) != 0 {
		t.Fatalf("expected empty slices, got prod: %v, dev: %v", prod, dev)
	}
}

func TestParseComposer_ComposerJSONOnly(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composer_json_only_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	composerJSON := `{
		"require": {
			"php": ">=8.2",
			"ext-json": "*",
			"symfony/http-kernel": "^6.4",
			"psr/log": "^3.0"
		},
		"require-dev": {
			"phpunit/phpunit": "^10.0",
			"symfony/maker-bundle": "^1.50"
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(composerJSON), 0644); err != nil {
		t.Fatal(err)
	}

	prod, dev, err := ParseComposer(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedProd := []string{"psr/log", "symfony/http-kernel"}
	expectedDev := []string{"phpunit/phpunit", "symfony/maker-bundle"}

	if !reflect.DeepEqual(prod, expectedProd) {
		t.Errorf("prod packages mismatch: got %v, want %v", prod, expectedProd)
	}
	if !reflect.DeepEqual(dev, expectedDev) {
		t.Errorf("dev packages mismatch: got %v, want %v", dev, expectedDev)
	}
}

func TestParseComposer_ComposerLockTransitiveDev(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composer_lock_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	composerJSON := `{
		"require": {
			"symfony/framework-bundle": "^7.0"
		},
		"require-dev": {
			"zenstruck/foundry": "^2.0"
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(composerJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// composer.lock contains transitive dependencies: fakerphp/faker in packages-dev
	composerLock := `{
		"packages": [
			{"name": "symfony/framework-bundle"},
			{"name": "symfony/http-kernel"}
		],
		"packages-dev": [
			{"name": "zenstruck/foundry"},
			{"name": "fakerphp/faker"}
		]
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "composer.lock"), []byte(composerLock), 0644); err != nil {
		t.Fatal(err)
	}

	prod, dev, err := ParseComposer(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedProd := []string{"symfony/framework-bundle", "symfony/http-kernel"}
	expectedDev := []string{"fakerphp/faker", "zenstruck/foundry"}

	if !reflect.DeepEqual(prod, expectedProd) {
		t.Errorf("prod packages mismatch: got %v, want %v", prod, expectedProd)
	}
	if !reflect.DeepEqual(dev, expectedDev) {
		t.Errorf("dev packages mismatch: got %v, want %v", dev, expectedDev)
	}
}

func TestParseComposer_InstalledJSONTransitiveDev(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composer_installed_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	composerJSON := `{
		"require": {
			"symfony/http-kernel": "^6.4"
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(composerJSON), 0644); err != nil {
		t.Fatal(err)
	}

	installedDir := filepath.Join(tmpDir, "vendor", "composer")
	if err := os.MkdirAll(installedDir, 0755); err != nil {
		t.Fatal(err)
	}

	installedJSON := `{
		"packages": [],
		"dev-package-names": [
			"fakerphp/faker",
			"nelmio/alice"
		]
	}`
	if err := os.WriteFile(filepath.Join(installedDir, "installed.json"), []byte(installedJSON), 0644); err != nil {
		t.Fatal(err)
	}

	prod, dev, err := ParseComposer(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedProd := []string{"symfony/http-kernel"}
	expectedDev := []string{"fakerphp/faker", "nelmio/alice"}

	if !reflect.DeepEqual(prod, expectedProd) {
		t.Errorf("prod packages mismatch: got %v, want %v", prod, expectedProd)
	}
	if !reflect.DeepEqual(dev, expectedDev) {
		t.Errorf("dev packages mismatch: got %v, want %v", dev, expectedDev)
	}
}

func TestParseComposer_DevPackageConflictWithProd(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composer_conflict_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// A package is mistakenly in require-dev but also resolved as production dependency
	composerJSON := `{
		"require": {
			"symfony/http-kernel": "^6.4"
		},
		"require-dev": {
			"psr/log": "^3.0"
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(composerJSON), 0644); err != nil {
		t.Fatal(err)
	}

	composerLock := `{
		"packages": [
			{"name": "symfony/http-kernel"},
			{"name": "psr/log"}
		],
		"packages-dev": [
			{"name": "phpunit/phpunit"}
		]
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "composer.lock"), []byte(composerLock), 0644); err != nil {
		t.Fatal(err)
	}

	prod, dev, err := ParseComposer(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// psr/log MUST be in prod, NOT in dev
	expectedProd := []string{"psr/log", "symfony/http-kernel"}
	expectedDev := []string{"phpunit/phpunit"}

	if !reflect.DeepEqual(prod, expectedProd) {
		t.Errorf("prod packages mismatch: got %v, want %v", prod, expectedProd)
	}
	if !reflect.DeepEqual(dev, expectedDev) {
		t.Errorf("dev packages mismatch: got %v, want %v", dev, expectedDev)
	}
}

func TestParseComposer_CaseInsensitiveNormalization(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composer_case_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	composerJSON := `{
		"require-dev": {
			"FakerPHP/Faker": "^1.24"
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(composerJSON), 0644); err != nil {
		t.Fatal(err)
	}

	_, dev, err := ParseComposer(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDev := []string{"fakerphp/faker"}
	if !reflect.DeepEqual(dev, expectedDev) {
		t.Errorf("dev packages mismatch: got %v, want %v", dev, expectedDev)
	}
}

func TestParseComposer_CorruptedLockFallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composer_corrupted_lock_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	composerJSON := `{
		"require": {
			"symfony/http-kernel": "^6.4"
		},
		"require-dev": {
			"phpunit/phpunit": "^10.0"
		}
	}`
	if err := os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(composerJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Corrupted lock file
	if err := os.WriteFile(filepath.Join(tmpDir, "composer.lock"), []byte(`{invalid json`), 0644); err != nil {
		t.Fatal(err)
	}

	prod, dev, err := ParseComposer(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedProd := []string{"symfony/http-kernel"}
	expectedDev := []string{"phpunit/phpunit"}

	if !reflect.DeepEqual(prod, expectedProd) {
		t.Errorf("prod packages mismatch: got %v, want %v", prod, expectedProd)
	}
	if !reflect.DeepEqual(dev, expectedDev) {
		t.Errorf("dev packages mismatch: got %v, want %v", dev, expectedDev)
	}
}

func TestParseComposer_CorruptedComposerJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composer_corrupted_json_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte(`{invalid json`), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err = ParseComposer(tmpDir)
	if err == nil {
		t.Fatal("expected error for corrupted composer.json, got nil")
	}
}
