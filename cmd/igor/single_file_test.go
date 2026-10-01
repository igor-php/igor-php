package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAuditBaseline_DefaultFileAutoLoaded(t *testing.T) {
	tmpDir := t.TempDir()
	writeTestFile(t, filepath.Join(tmpDir, "igor-baseline.json"), `{"files": {"src/Default.php": [{"message": "default"}]}}`)
	writeTestFile(t, filepath.Join(tmpDir, "custom-baseline.json"), `{"files": {"src/Custom.php": [{"message": "custom"}]}}`)

	// 1. No baseline configured: igor-baseline.json at the project root is loaded
	cfg := config.Config{}
	baseline := loadAuditBaseline(tmpDir, &cfg)
	if _, found := baseline.Files["src/Default.php"]; !found {
		t.Errorf("Expected default igor-baseline.json to be auto-loaded, got %v", baseline.Files)
	}

	// 2. An explicit baseline path takes precedence over the default file
	cfgCustom := config.Config{BaselinePath: "custom-baseline.json"}
	baselineCustom := loadAuditBaseline(tmpDir, &cfgCustom)
	if _, found := baselineCustom.Files["src/Custom.php"]; !found {
		t.Errorf("Expected custom baseline to be loaded, got %v", baselineCustom.Files)
	}
	if _, found := baselineCustom.Files["src/Default.php"]; found {
		t.Errorf("Expected default baseline to be ignored when a custom path is set")
	}

	// 3. No default file: the baseline is empty but usable
	emptyDir := t.TempDir()
	baselineEmpty := loadAuditBaseline(emptyDir, &config.Config{})
	if baselineEmpty.Files == nil || len(baselineEmpty.Files) != 0 {
		t.Errorf("Expected an empty baseline without default file, got %v", baselineEmpty.Files)
	}
}

func TestLoadAuditBaseline_SingleFileVendorTarget(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "vendor", "acme", "foo", "src", "Foo.php")
	writeTestFile(t, targetFile, "<?php class Foo {}")
	writeTestFile(t, filepath.Join(tmpDir, "vendor", "acme", "foo", "igor-baseline.json"), `{"files": {"src/Foo.php": [{"message": "foo"}]}}`)
	writeTestFile(t, filepath.Join(tmpDir, "vendor", "acme", "bar", "igor-baseline.json"), `{"files": {"src/Bar.php": [{"message": "bar"}]}}`)

	fooKey := filepath.Join("vendor", "acme", "foo", "src", "Foo.php")
	barKey := filepath.Join("vendor", "acme", "bar", "src", "Bar.php")

	// 1. Only the baseline of the target's package is merged
	cfg := config.Config{TargetFile: targetFile}
	baseline := loadAuditBaseline(tmpDir, &cfg)
	if _, found := baseline.Files[fooKey]; !found {
		t.Errorf("Expected target package baseline to be merged under %s, got %v", fooKey, baseline.Files)
	}
	if _, found := baseline.Files[barKey]; found {
		t.Errorf("Expected other vendor packages not to be scanned for a single-file target")
	}

	// 2. --ignore-external-baseline disables it
	cfgIgnore := config.Config{TargetFile: targetFile, IgnoreExternalBaseline: true}
	if baselineIgnore := loadAuditBaseline(tmpDir, &cfgIgnore); len(baselineIgnore.Files) != 0 {
		t.Errorf("Expected no external baseline with IgnoreExternalBaseline, got %v", baselineIgnore.Files)
	}

	// 3. A project file target does not load vendor baselines
	projectFile := filepath.Join(tmpDir, "src", "App.php")
	writeTestFile(t, projectFile, "<?php class App {}")
	cfgProject := config.Config{TargetFile: projectFile}
	if baselineProject := loadAuditBaseline(tmpDir, &cfgProject); len(baselineProject.Files) != 0 {
		t.Errorf("Expected no vendor baseline for a project file target, got %v", baselineProject.Files)
	}
}

func TestCli_ParseFlags_FileTarget(t *testing.T) {
	tmpDir := t.TempDir()
	writeTestFile(t, filepath.Join(tmpDir, "composer.json"), "{}")
	targetFile := filepath.Join(tmpDir, "src", "Service", "Foo.php")
	writeTestFile(t, targetFile, "<?php class Foo {}")

	cfg, rootPath, shouldExit, err := parseFlagsAndInit([]string{"igor", targetFile})
	if err != nil || shouldExit {
		t.Fatalf("parseFlagsAndInit failed: err=%v shouldExit=%v", err, shouldExit)
	}
	if cfg.TargetFile != targetFile {
		t.Errorf("Expected TargetFile %s, got %s", targetFile, cfg.TargetFile)
	}
	if rootPath != tmpDir {
		t.Errorf("Expected project root %s, got %s", tmpDir, rootPath)
	}

	// A directory target keeps the directory audit mode
	cfgDir, rootDir, _, err := parseFlagsAndInit([]string{"igor", tmpDir})
	if err != nil {
		t.Fatalf("parseFlagsAndInit failed: %v", err)
	}
	if cfgDir.TargetFile != "" || rootDir != tmpDir {
		t.Errorf("Expected directory audit of %s, got TargetFile=%q root=%s", tmpDir, cfgDir.TargetFile, rootDir)
	}

	// Baseline management flags are rejected with a file target
	if _, _, _, err := parseFlagsAndInit([]string{"igor", "--prune-baseline", targetFile}); err == nil {
		t.Errorf("Expected --prune-baseline to be rejected with a single file target")
	}
}

func TestFindProjectRoot_Markers(t *testing.T) {
	tests := []struct {
		name   string
		marker string
	}{
		{"bin/console", filepath.Join("bin", "console")},
		{"igor.json", "igor.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			writeTestFile(t, filepath.Join(tmpDir, tt.marker), "")
			deepDir := filepath.Join(tmpDir, "src", "Service")
			_ = os.MkdirAll(deepDir, 0755)

			if root := findProjectRoot(deepDir); root != tmpDir {
				t.Errorf("Expected root %s from marker %s, got %s", tmpDir, tt.marker, root)
			}
		})
	}
}

func TestFindProjectRoot_NestedVendorWithoutOuterComposer(t *testing.T) {
	tmpDir := t.TempDir()
	outerPkgDir := filepath.Join(tmpDir, "vendor", "acme", "foo")
	innerSrc := filepath.Join(outerPkgDir, "vendor", "bar", "baz", "src")
	_ = os.MkdirAll(innerSrc, 0755)
	writeTestFile(t, filepath.Join(outerPkgDir, "composer.json"), "{}")

	// The outermost candidate has no composer.json, so the next vendor/ segment is tried
	if root := findProjectRoot(innerSrc); root != outerPkgDir {
		t.Errorf("Expected root %s, got %s", outerPkgDir, root)
	}
}

func TestFindProjectRoot_ProjectUnderVendorNamedDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	appDir := filepath.Join(tmpDir, "vendor", "my-app")
	writeTestFile(t, filepath.Join(appDir, "composer.json"), "{}")
	srcDir := filepath.Join(appDir, "src", "Service")
	_ = os.MkdirAll(srcDir, 0755)

	// No host project contains this vendor/ directory: the application itself is the root
	if root := findProjectRoot(srcDir); root != appDir {
		t.Errorf("Expected root %s, got %s", appDir, root)
	}
}

func TestCollectFiles_SingleFile_SymfonySkippedCases(t *testing.T) {
	tmpDir := t.TempDir()
	safeParent := filepath.Join(tmpDir, "src", "Safe", "BaseHandler.php")
	excludedFile := filepath.Join(tmpDir, "src", "Service", "LegacyService.php")
	otherFile := filepath.Join(tmpDir, "src", "Service", "Moved.php")
	writeTestFile(t, safeParent, "<?php namespace Lib\\Safe; abstract class BaseHandler {}")
	writeTestFile(t, excludedFile, "<?php namespace App\\Service; class LegacyService {}")
	writeTestFile(t, otherFile, "<?php namespace App\\Service; class Moved {}")

	cfg := config.Config{SafeNamespaces: []string{"Lib\\Safe\\"}, Verbose: true}
	aud := auditor.NewAuditor(cfg)
	bridge := auditor.NewSymfonyBridge(tmpDir, "bin/console", cfg)
	bridge.Container = &symbol.SymfonyContainer{
		Definitions: map[string]symbol.SymfonyService{
			"app.legacy": {
				Class:  "App\\Service\\LegacyService",
				Shared: true,
				Tags:   []any{map[string]any{"name": "container.excluded"}},
			},
		},
	}
	bridge.ClassToFile = map[string]string{
		"Lib\\Safe\\BaseHandler": safeParent,
		// Reflection maps this class to another file than the one being edited
		"App\\Service\\Moved": filepath.Join(tmpDir, "src", "Old", "Moved.php"),
	}
	aud.Symfony = bridge

	cases := map[string]string{
		"parent class in a safe namespace":    safeParent,
		"service tagged container.excluded":   excludedFile,
		"reflection maps class to other file": otherFile,
	}
	for name, file := range cases {
		t.Run(name, func(t *testing.T) {
			list := collectFiles(tmpDir, config.Config{TargetFile: file, SafeNamespaces: cfg.SafeNamespaces, Verbose: true}, aud)
			if len(list) != 0 {
				t.Errorf("Expected %s to be skipped, got %+v", file, list)
			}
		})
	}
}
