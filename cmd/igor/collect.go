package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

func collectFiles(rootPath string, cfg config.Config, aud *auditor.Auditor) []symbol.AuditStatus {
	if cfg.TargetFile != "" {
		return collectSingleFile(rootPath, cfg.TargetFile, cfg, aud)
	}

	var auditList []symbol.AuditStatus
	processedFiles := make(map[string]bool)

	// --- STEP 1: Add shared services from Symfony (to get IDs and Dependencies) ---
	// Guard the Container too: a non-nil bridge can still carry a nil Container if
	// LoadContainer failed, and collectSymfonyServices iterates Container.Definitions.
	if aud.Symfony != nil && aud.Symfony.Container != nil {
		fmt.Fprintln(os.Stderr, "🎯 Symfony detected: Mapping services and dependencies...")
		auditList = append(auditList, collectSymfonyServices(rootPath, cfg, aud, processedFiles)...)
		// Also collect parent classes and traits resolved by reflection that are not yet processed
		for class, path := range aud.Symfony.ClassToFile {
			if processedFiles[path] {
				continue
			}
			if aud.IsSafeNamespace(class) {
				continue
			}
			if aud.Symfony.IsExcludedService(class) {
				continue
			}
			if skip, _ := shouldSkipServicePath("", path, cfg, aud, rootPath); skip {
				continue
			}
			auditList = append(auditList, symbol.AuditStatus{
				ServiceID: "Inherited/" + class,
				FilePath:  path,
				Status:    "⏳ PENDING",
				IsShared:  true,
			})
			processedFiles[path] = true
		}
	}

	// --- STEP 2: Scan remaining local project files ---
	// If Symfony is detected, we skip scanning the entire directory as we only audit
	// registered services and their dependencies to avoid transient file false-positives.
	if aud.Symfony == nil || aud.Symfony.Container == nil {
		auditList = append(auditList, collectLocalFiles(rootPath, cfg, aud, processedFiles)...)
	}

	// --- STEP 3: Forced Vendor Scan ---
	if len(cfg.ScanVendors) > 0 {
		fmt.Fprintln(os.Stderr, "🔍 Forced Vendor Scan: Auditing specific vendor paths...")
		auditList = append(auditList, collectForcedVendorFiles(rootPath, cfg, processedFiles)...)
	}
	return auditList
}

func collectLocalFiles(rootPath string, cfg config.Config, aud *auditor.Auditor, processed map[string]bool) []symbol.AuditStatus {
	var list []symbol.AuditStatus
	_ = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".php") {
			return nil
		}

		if skip, _ := shouldSkipServicePath("", path, cfg, aud, rootPath); skip {
			return nil
		}

		// Avoid files starting with . (like .php-cs-fixer.dist.php)
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}

		if !processed[path] {
			list = append(list, symbol.AuditStatus{ServiceID: "N/A", FilePath: path, Status: "⏳ PENDING"})
			processed[path] = true
		}
		return nil
	})
	return list
}

func shouldSkipServiceMeta(id string, def symbol.SymfonyService, aud *auditor.Auditor) (bool, string) {
	if strings.HasPrefix(id, ".errored.") {
		return true, "container error"
	}
	if !def.Shared {
		return true, "non-shared (prototype)"
	}
	if def.Class == "" {
		return true, "no class defined"
	}
	if def.IsExcluded() {
		return true, "tagged as container.excluded"
	}
	if aud.IsSafeNamespace(def.Class) {
		return true, fmt.Sprintf("class %s belongs to a safe namespace", def.Class)
	}
	return false, ""
}

func shouldSkipServicePath(_ string, path string, cfg config.Config, aud *auditor.Auditor, rootPath string) (bool, string) {
	if cfg.IsExcluded(path, rootPath) {
		return true, fmt.Sprintf("path %s is excluded", path)
	}
	if aud.IsDevPackagePath(path, rootPath) {
		return true, "belongs to a dev package"
	}
	if cfg.IgnoreVendors && (symbol.AuditStatus{FilePath: path}).IsVendor(rootPath) {
		return true, "belongs to vendor directory"
	}
	return false, ""
}

func collectSymfonyServices(rootPath string, cfg config.Config, aud *auditor.Auditor, processed map[string]bool) []symbol.AuditStatus {
	var list []symbol.AuditStatus
	for id, def := range aud.Symfony.Container.Definitions {
		if skip, reason := shouldSkipServiceMeta(id, def, aud); skip {
			if cfg.Verbose {
				fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': %s\n", id, reason)
			}
			continue
		}

		if path, found := aud.Symfony.ClassToFile[def.Class]; found {
			if skip, reason := shouldSkipServicePath(id, path, cfg, aud, rootPath); skip {
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': %s\n", id, reason)
				}
				continue
			}
			if !processed[path] {
				deps := extractDependencies(def)
				list = append(list, symbol.AuditStatus{
					ServiceID:    id,
					FilePath:     path,
					Status:       "⏳ PENDING",
					Dependencies: deps,
					IsShared:     def.Shared,
					IsPublic:     def.Public,
				})
				processed[path] = true
			} else if cfg.Verbose {
				fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': file already scheduled for audit\n", id)
			}
		} else if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': could not locate file for class %s\n", id, def.Class)
		}
	}

	return list
}

func extractDependencies(def symbol.SymfonyService) []string {
	deps := []string{}
	for _, arg := range def.Arguments {
		if m, ok := arg.(map[string]any); ok {
			// In Symfony JSON, service arguments look like {"type": "service", "id": "..."}
			if typeVal, ok := m["type"].(string); ok && typeVal == "service" {
				if idVal, ok := m["id"].(string); ok {
					deps = append(deps, idVal)
				}
			}
		} else if s, ok := arg.(string); ok && strings.HasPrefix(s, "@") {
			// Fallback for simple string references (e.g. @logger)
			deps = append(deps, strings.TrimPrefix(s, "@"))
		}
	}
	return deps
}

func collectForcedVendorFiles(rootPath string, cfg config.Config, processed map[string]bool) []symbol.AuditStatus {
	var list []symbol.AuditStatus
	for _, vendorSubPath := range cfg.ScanVendors {
		fullVendorPath := filepath.Join(rootPath, "vendor", vendorSubPath)
		_ = filepath.Walk(fullVendorPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".php") {
				return nil
			}
			if !processed[path] {
				list = append(list, symbol.AuditStatus{ServiceID: "N/A", FilePath: path, Status: "⏳ PENDING"})
				processed[path] = true
			}
			return nil
		})
	}
	return list
}

func collectSingleFile(rootPath string, targetFile string, cfg config.Config, aud *auditor.Auditor) []symbol.AuditStatus {
	cleanTarget, err := filepath.Abs(targetFile)
	if err != nil {
		cleanTarget = targetFile
	}

	if skip, reason := shouldSkipServicePath("", cleanTarget, cfg, aud, rootPath); skip {
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "  ⏭️  Skipped file '%s': %s\n", cleanTarget, reason)
		}
		return nil
	}

	fqcn, _ := aud.ExtractFQCN(cleanTarget)
	if fqcn == "" {
		fqcn = filepath.Base(cleanTarget)
	}

	// If Symfony is detected, only audit the file if it backs a shared service
	if aud.Symfony != nil && aud.Symfony.Container != nil {
		return collectSingleSymfonyFile(cleanTarget, fqcn, cfg, aud)
	}

	return []symbol.AuditStatus{{
		ServiceID: fqcn,
		FilePath:  cleanTarget,
		Status:    "⏳ PENDING",
		IsShared:  true,
	}}
}

func collectSingleSymfonyFile(cleanTarget, fqcn string, cfg config.Config, aud *auditor.Auditor) []symbol.AuditStatus {
	if serviceID, matchedDef := findTargetDefinition(fqcn, aud); matchedDef != nil {
		if skip, reason := shouldSkipServiceMeta(serviceID, *matchedDef, aud); skip {
			if cfg.Verbose {
				fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': %s\n", serviceID, reason)
			}
			return nil
		}
		return []symbol.AuditStatus{{
			ServiceID:    serviceID,
			FilePath:     cleanTarget,
			Status:       "⏳ PENDING",
			Dependencies: extractDependencies(*matchedDef),
			IsShared:     matchedDef.Shared,
			IsPublic:     matchedDef.Public,
		}}
	}

	// Match parent classes and traits by the class the file (or editor buffer) declares,
	// not by path: an unsaved rename must not inherit the saved class's mapping.
	if path, found := aud.Symfony.ClassToFile[fqcn]; found && filepath.Clean(path) == cleanTarget {
		if aud.IsSafeNamespace(fqcn) || aud.Symfony.IsExcludedService(fqcn) {
			return nil
		}
		return []symbol.AuditStatus{{
			ServiceID: "Inherited/" + fqcn,
			FilePath:  cleanTarget,
			Status:    "⏳ PENDING",
			IsShared:  true,
		}}
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "  ⏭️  Skipped file '%s': not a registered Symfony shared service\n", cleanTarget)
	}
	return nil
}

// findTargetDefinition returns the definition backing fqcn, preferring an active shared one
// so that map iteration order cannot hide it behind an excluded or non-shared definition.
func findTargetDefinition(fqcn string, aud *auditor.Auditor) (string, *symbol.SymfonyService) {
	var matchedDef *symbol.SymfonyService
	var serviceID string
	for id, def := range aud.Symfony.Container.Definitions {
		if def.Class != fqcn && id != fqcn {
			continue
		}
		d := def
		if skip, _ := shouldSkipServiceMeta(id, def, aud); !skip {
			return id, &d
		}
		if matchedDef == nil {
			matchedDef = &d
			serviceID = id
		}
	}
	return serviceID, matchedDef
}
