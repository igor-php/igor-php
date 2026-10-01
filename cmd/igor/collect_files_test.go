package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

func TestCollectFiles_SymfonyActive_SkipsLocalFiles(t *testing.T) {
	// 1. Create a temporary project directory
	tmpDir, err := os.MkdirTemp("", "collect_files_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create src dir: %v", err)
	}

	// 2. Create a service file and a non-service (entity) file
	servicePath := filepath.Join(srcDir, "MyService.php")
	if err := os.WriteFile(servicePath, []byte("<?php class MyService {}"), 0644); err != nil {
		t.Fatalf("Failed to write service file: %v", err)
	}

	entityPath := filepath.Join(srcDir, "MyEntity.php")
	if err := os.WriteFile(entityPath, []byte("<?php class MyEntity {}"), 0644); err != nil {
		t.Fatalf("Failed to write entity file: %v", err)
	}

	cfg := config.Config{
		NoAgent: true,
	}

	// 3. Scenario A: Symfony bridge is ACTIVE
	aud := auditor.NewAuditor(cfg)
	bridge := auditor.NewSymfonyBridge(tmpDir, "bin/console", cfg)
	bridge.Container = &symbol.SymfonyContainer{
		Definitions: map[string]symbol.SymfonyService{
			"app.my_service": {
				Class:  "App\\Service\\MyService",
				Public: true,
				Shared: true,
			},
		},
	}
	bridge.ClassToFile = map[string]string{
		"App\\Service\\MyService": servicePath,
	}
	aud.Symfony = bridge

	auditList := collectFiles(tmpDir, cfg, aud)

	// We expect only MyService.php to be audited (from Symfony bridge).
	// MyEntity.php must be skipped since it's not a service and local scan is disabled when Symfony is detected.
	hasService := false
	hasEntity := false
	for _, item := range auditList {
		if filepath.Base(item.FilePath) == "MyService.php" {
			hasService = true
		}
		if filepath.Base(item.FilePath) == "MyEntity.php" {
			hasEntity = true
		}
	}

	if !hasService {
		t.Error("Expected MyService.php to be audited as a registered Symfony service, but it was skipped")
	}
	if hasEntity {
		t.Error("Expected MyEntity.php to be skipped when Symfony is active, but it was collected for audit")
	}

	// 4. Scenario B: Symfony bridge is INACTIVE (non-Symfony project)
	audInactive := auditor.NewAuditor(cfg) // audInactive.Symfony is nil

	auditListInactive := collectFiles(tmpDir, cfg, audInactive)

	// We expect BOTH files to be collected via standard directory scan
	hasServiceInactive := false
	hasEntityInactive := false
	for _, item := range auditListInactive {
		if filepath.Base(item.FilePath) == "MyService.php" {
			hasServiceInactive = true
		}
		if filepath.Base(item.FilePath) == "MyEntity.php" {
			hasEntityInactive = true
		}
	}

	if !hasServiceInactive || !hasEntityInactive {
		t.Errorf("Expected both files to be collected when Symfony is inactive. Got MyService.php: %t, MyEntity.php: %t", hasServiceInactive, hasEntityInactive)
	}
}

func TestCollectFiles_SkipsSafeNamespacesInherited(t *testing.T) {
	// 1. Create a temporary project directory
	tmpDir, err := os.MkdirTemp("", "collect_files_safe_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create src dir: %v", err)
	}

	servicePath := filepath.Join(srcDir, "MyService.php")
	if err := os.WriteFile(servicePath, []byte("<?php class MyService {}"), 0644); err != nil {
		t.Fatalf("Failed to write service file: %v", err)
	}

	inheritedPath := filepath.Join(srcDir, "MyInheritedSafeService.php")
	if err := os.WriteFile(inheritedPath, []byte("<?php class MyInheritedSafeService {}"), 0644); err != nil {
		t.Fatalf("Failed to write inherited file: %v", err)
	}

	cfg := config.Config{
		NoAgent:        true,
		SafeNamespaces: []string{"Symfony\\", "App\\Safe\\"},
	}

	aud := auditor.NewAuditor(cfg)
	bridge := auditor.NewSymfonyBridge(tmpDir, "bin/console", cfg)
	bridge.Container = &symbol.SymfonyContainer{
		Definitions: map[string]symbol.SymfonyService{
			"app.my_service": {
				Class:  "App\\Service\\MyService",
				Public: true,
				Shared: true,
			},
		},
	}
	bridge.ClassToFile = map[string]string{
		"App\\Service\\MyService":           servicePath,
		"App\\Safe\\MyInheritedSafeService": inheritedPath,
	}
	aud.Symfony = bridge

	auditList := collectFiles(tmpDir, cfg, aud)

	// App\\Safe\\MyInheritedSafeService must be skipped since it is in a safe namespace.
	hasService := false
	hasInherited := false
	for _, item := range auditList {
		if filepath.Base(item.FilePath) == "MyService.php" {
			hasService = true
		}
		if filepath.Base(item.FilePath) == "MyInheritedSafeService.php" {
			hasInherited = true
		}
	}

	if !hasService {
		t.Error("Expected MyService.php to be collected")
	}
	if hasInherited {
		t.Error("Expected App\\Safe\\MyInheritedSafeService to be skipped due to safe namespace prefix, but it was collected")
	}
}

func TestCollectFiles_SkipsExcludedServices(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "collect_files_excluded_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create src dir: %v", err)
	}

	servicePath := filepath.Join(srcDir, "MyService.php")
	if err := os.WriteFile(servicePath, []byte("<?php class MyService {}"), 0644); err != nil {
		t.Fatalf("Failed to write service file: %v", err)
	}

	excludedPath := filepath.Join(srcDir, "LeakyService.php")
	if err := os.WriteFile(excludedPath, []byte("<?php class LeakyService {}"), 0644); err != nil {
		t.Fatalf("Failed to write excluded file: %v", err)
	}

	cfg := config.Config{
		NoAgent: true,
	}

	aud := auditor.NewAuditor(cfg)
	bridge := auditor.NewSymfonyBridge(tmpDir, "bin/console", cfg)
	bridge.Container = &symbol.SymfonyContainer{
		Definitions: map[string]symbol.SymfonyService{
			"app.my_service": {
				Class:  "App\\Service\\MyService",
				Public: true,
				Shared: true,
			},
			"app.leaky_service": {
				Class:  "App\\Service\\LeakyService",
				Public: true,
				Shared: true,
				Tags: []any{
					map[string]any{"name": "container.excluded"},
				},
			},
		},
	}
	bridge.ClassToFile = map[string]string{
		"App\\Service\\MyService":    servicePath,
		"App\\Service\\LeakyService": excludedPath,
	}
	aud.Symfony = bridge

	auditList := collectFiles(tmpDir, cfg, aud)

	hasService := false
	hasExcluded := false
	for _, item := range auditList {
		if filepath.Base(item.FilePath) == "MyService.php" {
			hasService = true
		}
		if filepath.Base(item.FilePath) == "LeakyService.php" {
			hasExcluded = true
		}
	}

	if !hasService {
		t.Error("Expected MyService.php to be collected for audit")
	}
	if hasExcluded {
		t.Error("Expected LeakyService.php to be skipped due to container.excluded tag, but it was collected")
	}
}

func TestCollectFiles_SameFile_ExcludedAndActive(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "collect_files_same_file_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	srcDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatalf("Failed to create src dir: %v", err)
	}

	servicePath := filepath.Join(srcDir, "MultiService.php")
	if err := os.WriteFile(servicePath, []byte("<?php class MultiService {}"), 0644); err != nil {
		t.Fatalf("Failed to write service file: %v", err)
	}

	cfg := config.Config{
		NoAgent: true,
	}

	// Run multiple times to verify map iteration order doesn't cause active service to be dropped
	for i := 0; i < 20; i++ {
		aud := auditor.NewAuditor(cfg)
		bridge := auditor.NewSymfonyBridge(tmpDir, "bin/console", cfg)
		bridge.Container = &symbol.SymfonyContainer{
			Definitions: map[string]symbol.SymfonyService{
				"app.excluded_definition": {
					Class:  "App\\Service\\MultiService",
					Public: true,
					Shared: true,
					Tags: []any{
						map[string]any{"name": "container.excluded"},
					},
				},
				"app.active_definition": {
					Class:  "App\\Service\\MultiService",
					Public: true,
					Shared: true,
				},
			},
		}
		bridge.ClassToFile = map[string]string{
			"App\\Service\\MultiService": servicePath,
		}
		aud.Symfony = bridge

		auditList := collectFiles(tmpDir, cfg, aud)

		hasService := false
		for _, item := range auditList {
			if filepath.Base(item.FilePath) == "MultiService.php" {
				hasService = true
				break
			}
		}

		if !hasService {
			t.Fatalf("Iteration %d: Expected MultiService.php to be collected for audit via active definition despite excluded definition on same file", i)
		}
	}
}

func TestCollectFiles_SingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	serviceFile := filepath.Join(tmpDir, "src", "Service", "SingleService.php")
	if err := os.MkdirAll(filepath.Dir(serviceFile), 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	content := `<?php
namespace App\Service;
class SingleService {
    private $state = [];
    public function mutate() { $this->state[] = 1; }
}
`
	if err := os.WriteFile(serviceFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write service file: %v", err)
	}

	cfg := config.Config{
		TargetFile: serviceFile,
	}
	aud := auditor.NewAuditor(cfg)

	list := collectFiles(tmpDir, cfg, aud)
	if len(list) != 1 {
		t.Fatalf("Expected exactly 1 item in audit list, got %d", len(list))
	}
	if list[0].FilePath != serviceFile {
		t.Errorf("Expected FilePath %s, got %s", serviceFile, list[0].FilePath)
	}
}

func TestFindProjectRoot(t *testing.T) {
	tmpDir := t.TempDir()
	composerPath := filepath.Join(tmpDir, "composer.json")
	_ = os.WriteFile(composerPath, []byte("{}"), 0644)

	deepDir := filepath.Join(tmpDir, "src", "Sub", "Service")
	_ = os.MkdirAll(deepDir, 0755)

	foundRoot := findProjectRoot(deepDir)
	if foundRoot != tmpDir {
		t.Errorf("findProjectRoot(%s) = %s, expected %s", deepDir, foundRoot, tmpDir)
	}
}

func TestCli_ParseFlags_StdinFilepathAndIncludeIgnored(t *testing.T) {
	relPath := filepath.Join("src", "Service", "VirtualService.php")
	args := []string{"igor", "--stdin-filepath", relPath, "--include-ignored", "."}
	cfg, _, shouldExit, err := parseFlagsAndInit(args)
	if err != nil {
		t.Fatalf("parseFlagsAndInit failed: %v", err)
	}
	if shouldExit {
		t.Fatalf("Expected shouldExit to be false, got true")
	}
	expectedAbs, _ := filepath.Abs(relPath)
	if cfg.TargetFile != expectedAbs {
		t.Errorf("Expected TargetFile %s, got %s", expectedAbs, cfg.TargetFile)
	}
	if !cfg.IncludeIgnored {
		t.Errorf("Expected IncludeIgnored to be true")
	}
}

func TestCollectFiles_SingleFile_WithFileOverride(t *testing.T) {
	tmpDir := t.TempDir()
	virtualFile := filepath.Join(tmpDir, "src", "Service", "InMemoryService.php")
	content := []byte(`<?php
namespace App\Service;
class InMemoryService {
    private $items = [];
    public function add($item) { $this->items[] = $item; }
}
`)

	cfg := config.Config{
		TargetFile: virtualFile,
	}
	aud := auditor.NewAuditor(cfg)
	aud.SetFileOverride(virtualFile, content)

	list := collectFiles(tmpDir, cfg, aud)
	if len(list) != 1 {
		t.Fatalf("Expected exactly 1 item in audit list, got %d", len(list))
	}
	if list[0].ServiceID != "App\\Service\\InMemoryService" {
		t.Errorf("Expected ServiceID App\\Service\\InMemoryService, got %s", list[0].ServiceID)
	}
}

func TestFindProjectRoot_InsideVendor(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte("{}"), 0644)
	vendorPkgDir := filepath.Join(tmpDir, "vendor", "acme", "foo")
	_ = os.MkdirAll(filepath.Join(vendorPkgDir, "src"), 0755)
	_ = os.WriteFile(filepath.Join(vendorPkgDir, "composer.json"), []byte("{}"), 0644)
	targetFile := filepath.Join(vendorPkgDir, "src", "Bar.php")

	foundRoot := findProjectRoot(filepath.Dir(targetFile))
	if foundRoot != tmpDir {
		t.Errorf("Expected host project root %s, got %s", tmpDir, foundRoot)
	}
}

func TestFindProjectRoot_NestedVendor(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, "composer.json"), []byte("{}"), 0644)
	outerPkgDir := filepath.Join(tmpDir, "vendor", "acme", "foo")
	innerPkgDir := filepath.Join(outerPkgDir, "vendor", "bar", "baz")
	_ = os.MkdirAll(filepath.Join(innerPkgDir, "src"), 0755)
	_ = os.WriteFile(filepath.Join(outerPkgDir, "composer.json"), []byte("{}"), 0644)
	_ = os.WriteFile(filepath.Join(innerPkgDir, "composer.json"), []byte("{}"), 0644)

	foundRoot := findProjectRoot(filepath.Join(innerPkgDir, "src"))
	if foundRoot != tmpDir {
		t.Errorf("Expected host project root %s, got %s", tmpDir, foundRoot)
	}
}

func TestCli_BaselineFlags_RejectsSingleFile(t *testing.T) {
	args := []string{"igor", "--generate-baseline", "--stdin-filepath", "src/Foo.php", "."}
	_, _, _, err := parseFlagsAndInit(args)
	if err == nil {
		t.Fatalf("Expected error when combining --generate-baseline and --stdin-filepath, got nil")
	}
}

func TestCollectFiles_SingleFile_SymfonyMultiDefAndUnregistered(t *testing.T) {
	tmpDir := t.TempDir()
	serviceFile := filepath.Join(tmpDir, "src", "Service", "DualService.php")
	_ = os.MkdirAll(filepath.Dir(serviceFile), 0755)
	_ = os.WriteFile(serviceFile, []byte("<?php namespace App\\Service; class DualService {}"), 0644)

	dtoFile := filepath.Join(tmpDir, "src", "DTO", "UserDTO.php")
	_ = os.MkdirAll(filepath.Dir(dtoFile), 0755)
	_ = os.WriteFile(dtoFile, []byte("<?php namespace App\\DTO; class UserDTO {}"), 0644)

	cfg := config.Config{}
	aud := auditor.NewAuditor(cfg)
	bridge := auditor.NewSymfonyBridge(tmpDir, "bin/console", cfg)
	bridge.Container = &symbol.SymfonyContainer{
		Definitions: map[string]symbol.SymfonyService{
			"app.dual_non_shared": {
				Class:  "App\\Service\\DualService",
				Shared: false,
			},
			"app.dual_shared": {
				Class:  "App\\Service\\DualService",
				Shared: true,
			},
		},
	}
	aud.Symfony = bridge

	// 1. Dual service should deterministically pick the shared definition
	cfgDual := config.Config{TargetFile: serviceFile}
	list := collectFiles(tmpDir, cfgDual, aud)
	if len(list) != 1 {
		t.Fatalf("Expected 1 item for DualService, got %d", len(list))
	}
	if !list[0].IsShared || list[0].ServiceID != "app.dual_shared" {
		t.Errorf("Expected shared definition app.dual_shared, got %s (shared=%v)", list[0].ServiceID, list[0].IsShared)
	}

	// 2. Unregistered DTO should return nil (skipped) in Symfony mode
	cfgDTO := config.Config{TargetFile: dtoFile}
	listDTO := collectFiles(tmpDir, cfgDTO, aud)
	if len(listDTO) != 0 {
		t.Errorf("Expected unregistered DTO to be skipped in Symfony mode, got %d items", len(listDTO))
	}
}

func TestEndToEnd_StdinBufferOverride_And_EmptyBuffer(t *testing.T) {
	tmpDir := t.TempDir()
	cleanFile := filepath.Join(tmpDir, "src", "Service", "CleanService.php")
	_ = os.MkdirAll(filepath.Dir(cleanFile), 0755)
	_ = os.WriteFile(cleanFile, []byte("<?php namespace App\\Service; class CleanService {}"), 0644)

	// 1. Unsaved buffer introduces a state mutation
	unsavedBuffer := []byte(`<?php
namespace App\Service;
class CleanService {
    private $state = 0;
    public function bump() { $this->state++; }
}
`)
	cfg := config.Config{TargetFile: cleanFile}
	aud := auditor.NewAuditor(cfg)
	aud.SetFileOverride(cleanFile, unsavedBuffer)

	auditList := collectFiles(tmpDir, cfg, aud)
	results := executeAudit(auditList, aud, cfg, config.Baseline{}, tmpDir)
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if len(results[0].Findings) == 0 {
		t.Errorf("Expected findings for unsaved editor buffer with mutation, got 0")
	}

	// 2. Unsaved buffer is empty ([]byte{}) -> should yield 0 findings, NOT read disk file
	audEmpty := auditor.NewAuditor(cfg)
	audEmpty.SetFileOverride(cleanFile, []byte{})
	resultsEmpty := executeAudit(auditList, audEmpty, cfg, config.Baseline{}, tmpDir)
	if len(resultsEmpty) != 1 {
		t.Fatalf("Expected 1 result for empty buffer, got %d", len(resultsEmpty))
	}
	if len(resultsEmpty[0].Findings) != 0 {
		t.Errorf("Expected 0 findings for empty buffer override, got %d", len(resultsEmpty[0].Findings))
	}
}

func TestEndToEnd_IncludeIgnored_StatusIsOK(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "src", "Service", "ExemptService.php")
	_ = os.MkdirAll(filepath.Dir(file), 0755)
	code := `<?php
namespace App\Service;
class ExemptService {
    private $count = 0;
    public function inc() { $this->count++; }
}
`
	_ = os.WriteFile(file, []byte(code), 0644)
	cfg := config.Config{
		TargetFile:     file,
		IncludeIgnored: true,
	}
	aud := auditor.NewAuditor(cfg)
	auditList := collectFiles(tmpDir, cfg, aud)

	// Pre-generate baseline containing this finding
	rawResults := executeAudit(auditList, aud, config.Config{TargetFile: file}, config.Baseline{}, tmpDir)
	if len(rawResults) == 0 || len(rawResults[0].Findings) == 0 {
		t.Fatalf("Expected raw findings, got none")
	}

	baseline := config.Baseline{
		Files: map[string][]config.BaselineEntry{
			"src/Service/ExemptService.php": {
				{Message: rawResults[0].Findings[0].Message, Reason: "Exempted legacy"},
			},
		},
	}

	results := executeAudit(auditList, aud, cfg, baseline, tmpDir)
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if len(results[0].Findings) == 0 {
		t.Fatalf("Expected findings to be retained with IncludeIgnored=true, got 0")
	}
	if !results[0].Findings[0].Ignored {
		t.Errorf("Expected finding to be marked Ignored=true")
	}
	// Status must be OK because active findings count is 0
	if results[0].Status != "✅ OK" {
		t.Errorf("Expected status '✅ OK' for baseline-ignored finding, got %s", results[0].Status)
	}
}

func TestCollectFiles_SingleFile_SymfonyInheritedMatchesDeclaredClass(t *testing.T) {
	tmpDir := t.TempDir()
	serviceFile := filepath.Join(tmpDir, "src", "Service", "FooService.php")
	baseFile := filepath.Join(tmpDir, "src", "Service", "BaseService.php")
	_ = os.MkdirAll(filepath.Dir(serviceFile), 0755)
	_ = os.WriteFile(serviceFile, []byte("<?php namespace App\\Service; class FooService extends BaseService {}"), 0644)
	_ = os.WriteFile(baseFile, []byte("<?php namespace App\\Service; abstract class BaseService {}"), 0644)

	newAuditor := func() *auditor.Auditor {
		aud := auditor.NewAuditor(config.Config{})
		bridge := auditor.NewSymfonyBridge(tmpDir, "bin/console", config.Config{})
		bridge.Container = &symbol.SymfonyContainer{
			Definitions: map[string]symbol.SymfonyService{
				"App\\Service\\FooService": {Class: "App\\Service\\FooService", Shared: true},
			},
		}
		bridge.ClassToFile = map[string]string{
			"App\\Service\\FooService":  serviceFile,
			"App\\Service\\BaseService": baseFile,
		}
		aud.Symfony = bridge
		return aud
	}

	// 1. A parent class of a shared service is audited through the reflection mapping
	list := collectFiles(tmpDir, config.Config{TargetFile: baseFile}, newAuditor())
	if len(list) != 1 || list[0].ServiceID != "Inherited/App\\Service\\BaseService" {
		t.Fatalf("Expected parent class to be audited as Inherited/App\\Service\\BaseService, got %+v", list)
	}

	// 2. An unsaved buffer renaming the service class must not reuse the saved class mapping
	aud := newAuditor()
	aud.SetFileOverride(serviceFile, []byte("<?php namespace App\\Service; class RenamedService extends BaseService {}"))
	listRenamed := collectFiles(tmpDir, config.Config{TargetFile: serviceFile}, aud)
	if len(listRenamed) != 0 {
		t.Errorf("Expected unsaved renamed class to be skipped, got %+v", listRenamed)
	}
}
