package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ComposerJSON represents a minimal composer.json structure.
type ComposerJSON struct {
	Require    map[string]string `json:"require"`
	RequireDev map[string]string `json:"require-dev"`
}

// ComposerLockPackage represents a package entry in composer.lock or installed.json.
type ComposerLockPackage struct {
	Name string `json:"name"`
}

// ComposerLock represents a minimal composer.lock structure.
type ComposerLock struct {
	Packages    []ComposerLockPackage `json:"packages"`
	PackagesDev []ComposerLockPackage `json:"packages-dev"`
}

// ComposerInstalled represents a minimal vendor/composer/installed.json structure (Composer 2+).
type ComposerInstalled struct {
	Packages        []ComposerLockPackage `json:"packages"`
	DevPackageNames []string              `json:"dev-package-names"`
}

func isValidPackageName(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" || !strings.Contains(name, "/") {
		return false
	}
	if name == "php" || strings.HasPrefix(name, "ext-") || strings.HasPrefix(name, "lib-") {
		return false
	}
	return true
}

func parseComposerJSON(root string, prodMap, devMap map[string]bool) (bool, error) {
	composerPath := filepath.Join(root, "composer.json")
	data, readErr := os.ReadFile(composerPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return false, nil
		}
		return false, readErr
	}

	var composer ComposerJSON
	if jsonErr := json.Unmarshal(data, &composer); jsonErr != nil {
		return false, jsonErr
	}

	for pkg := range composer.Require {
		pkg = strings.TrimSpace(strings.ToLower(pkg))
		if isValidPackageName(pkg) {
			prodMap[pkg] = true
		}
	}
	for pkg := range composer.RequireDev {
		pkg = strings.TrimSpace(strings.ToLower(pkg))
		if isValidPackageName(pkg) {
			devMap[pkg] = true
		}
	}
	return true, nil
}

func parseComposerLock(root string, prodMap, devMap map[string]bool) bool {
	lockPath := filepath.Join(root, "composer.lock")
	data, lockErr := os.ReadFile(lockPath)
	if lockErr != nil {
		return false
	}

	var lock ComposerLock
	if jsonErr := json.Unmarshal(data, &lock); jsonErr != nil {
		return false
	}

	for _, pkg := range lock.Packages {
		name := strings.TrimSpace(strings.ToLower(pkg.Name))
		if isValidPackageName(name) {
			prodMap[name] = true
		}
	}
	for _, pkg := range lock.PackagesDev {
		name := strings.TrimSpace(strings.ToLower(pkg.Name))
		if isValidPackageName(name) {
			devMap[name] = true
		}
	}
	return true
}

func parseInstalledJSON(root string, devMap map[string]bool) bool {
	installedPath := filepath.Join(root, "vendor", "composer", "installed.json")
	data, instErr := os.ReadFile(installedPath)
	if instErr != nil {
		return false
	}

	var installed ComposerInstalled
	if jsonErr := json.Unmarshal(data, &installed); jsonErr != nil {
		return false
	}

	for _, pkgName := range installed.DevPackageNames {
		name := strings.TrimSpace(strings.ToLower(pkgName))
		if isValidPackageName(name) {
			devMap[name] = true
		}
	}
	return true
}

// ParseComposer parses composer.json, composer.lock, and vendor/composer/installed.json
// to return comprehensive lists of production and dev packages (including transitive dependencies).
func ParseComposer(root string) (prod []string, dev []string, err error) {
	prodMap := make(map[string]bool)
	devMap := make(map[string]bool)

	foundJSON, err := parseComposerJSON(root, prodMap, devMap)
	if err != nil {
		return nil, nil, err
	}
	foundLock := parseComposerLock(root, prodMap, devMap)
	foundInstalled := parseInstalledJSON(root, devMap)

	if !foundJSON && !foundLock && !foundInstalled {
		return []string{}, []string{}, nil
	}

	// Ensure packages required in production are never classified as dev-only
	for pkg := range prodMap {
		delete(devMap, pkg)
	}

	prod = make([]string, 0, len(prodMap))
	for pkg := range prodMap {
		prod = append(prod, pkg)
	}
	sort.Strings(prod)

	dev = make([]string, 0, len(devMap))
	for pkg := range devMap {
		dev = append(dev, pkg)
	}
	sort.Strings(dev)

	return prod, dev, nil
}
