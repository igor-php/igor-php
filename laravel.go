package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed laravel_helper.php
var laravelHelperScript []byte

// LaravelBridge handles all interactions with the Laravel Framework.
type LaravelBridge struct {
	Root           string
	Config         Config
	ClassToFile    map[string]string
	FlushedClasses map[string]bool
}

// NewLaravelBridge creates a new bridge for a given project root.
func NewLaravelBridge(root string, config Config) *LaravelBridge {
	return &LaravelBridge{
		Root:           root,
		Config:         config,
		ClassToFile:    make(map[string]string),
		FlushedClasses: make(map[string]bool),
	}
}

// DetectLaravel attempts to find a Laravel project and initialize the bridge.
func DetectLaravel(rootPath string, config Config) (FrameworkBridge, error) {
	projectRoot := rootPath
	artisanPath := config.ConsolePath

	// Default to 'artisan' if not specified or if set to Symfony's default
	if artisanPath == "" || artisanPath == "bin/console" {
		artisanPath = "artisan"
	}

	// 1. Check if artisan exists at the specified path
	fullPath := filepath.Join(projectRoot, artisanPath)
	if _, err := os.Stat(fullPath); err != nil {
		return nil, nil // Not a Laravel project
	}

	// 2. If we found artisan, try to initialize the bridge
	fmt.Printf("🔍 Laravel project detected at %s. Initializing Deep Audit...\n", projectRoot)
	lb := NewLaravelBridge(projectRoot, config)

	if err := lb.LoadBridge(); err != nil {
		fmt.Printf("  ⚠️  Could not extract Laravel/Octane config: %v\n", err)
	}

	return lb, nil
}

// LoadBridge executes the helper script to extract framework info.
func (b *LaravelBridge) LoadBridge() error {
	tmpHelper, err := os.CreateTemp("", "igor_laravel_*.php")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpHelper.Name()) }()

	if _, err := tmpHelper.Write(laravelHelperScript); err != nil {
		return err
	}
	_ = tmpHelper.Close()

	cmd := exec.Command("php", tmpHelper.Name(), b.Root)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("PHP execution failed: %v\nOutput: %s", err, string(output))
	}

	var results struct {
		FlushedClasses []string          `json:"flushed_classes"`
		ClassMap       map[string]string `json:"class_map"`
	}

	if err := json.Unmarshal(output, &results); err != nil {
		return fmt.Errorf("failed to parse helper output: %v", err)
	}

	// Store results
	for _, className := range results.FlushedClasses {
		b.FlushedClasses[NormalizeClassName(className)] = true
	}
	b.ClassToFile = results.ClassMap

	if len(b.FlushedClasses) > 0 {
		fmt.Printf("⚡ Octane detected: %d services are automatically flushed.\n", len(b.FlushedClasses))
	}

	return nil
}

// GetName returns the name of the framework.
func (b *LaravelBridge) GetName() string {
	return "Laravel"
}

// GetDefinitions returns service definitions.
func (b *LaravelBridge) GetDefinitions() map[string]ServiceDefinition {
	// For Laravel, we'll consider everything in ClassToFile as a potential service
	// and mark it as shared unless it's in the flushed list.
	defs := make(map[string]ServiceDefinition)
	for class := range b.ClassToFile {
		defs[class] = ServiceDefinition{
			ID:     class,
			Class:  class,
			Shared: !b.FlushedClasses[NormalizeClassName(class)],
		}
	}
	return defs
}

// GetClassToFileMap returns the class-to-file mapping for Laravel.
func (b *LaravelBridge) GetClassToFileMap() map[string]string {
	return b.ClassToFile
}

// IsSharedService checks if a FQCN is a shared service (singleton) in Laravel.
// If it's in Octane's flush list, it's NOT shared across requests.
func (b *LaravelBridge) IsSharedService(className string) bool {
	className = NormalizeClassName(className)
	if _, flushed := b.FlushedClasses[className]; flushed {
		return false
	}
	return true
}
