package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed find_class_files.php
var phpHelperScript []byte

// SymfonyBridge handles all interactions with the Symfony Framework.
type SymfonyBridge struct {
	Root        string
	Container   *SymfonyContainer
	ClassToFile map[string]string
}

// NewSymfonyBridge creates a new bridge for a given project root.
func NewSymfonyBridge(root string) *SymfonyBridge {
	return &SymfonyBridge{
		Root:        root,
		ClassToFile: make(map[string]string),
	}
}

// LoadContainer fetches definitions and locates files via PHP Reflection in PROD mode.
func (b *SymfonyBridge) LoadContainer() error {
	consolePath := filepath.Join(b.Root, "bin", "console")
	
	// 1. Get definitions
	cmd := exec.Command("php", consolePath, "debug:container", "--format=json", "--show-hidden", "--env=prod", "--no-debug")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute debug:container: %v", err)
	}

	strOutput := string(output)
	start := strings.Index(strOutput, "{")
	end := strings.LastIndex(strOutput, "}")
	if start == -1 || end == -1 || end < start {
		return fmt.Errorf("could not find a valid JSON object in Symfony output")
	}
	jsonPart := strOutput[start : end+1]

	var container SymfonyContainer
	if err := json.Unmarshal([]byte(jsonPart), &container); err != nil {
		return fmt.Errorf("failed to parse Symfony container JSON: %v", err)
	}
	b.Container = &container

	// 2. Locate files
	tmpHelper, err := os.CreateTemp("", "igor_helper_*.php")
	if err != nil {
		return fmt.Errorf("failed to create temp helper file: %v", err)
	}
	defer os.Remove(tmpHelper.Name())

	if _, err := tmpHelper.Write(phpHelperScript); err != nil {
		return fmt.Errorf("failed to write to temp helper: %v", err)
	}
	if err := tmpHelper.Close(); err != nil {
		return fmt.Errorf("failed to close temp helper: %v", err)
	}

	reflectCmd := exec.Command("php", tmpHelper.Name(), b.Root)
	reflectCmd.Stdin = bytes.NewReader([]byte(jsonPart))
	
	reflectOutput, err := reflectCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to locate files via reflection: %v", err)
	}

	if err := json.Unmarshal(reflectOutput, &b.ClassToFile); err != nil {
		return fmt.Errorf("failed to parse reflection mapping: %v", err)
	}

	return nil
}

// IsSharedService checks if a FQCN is a shared service in Symfony.
func (b *SymfonyBridge) IsSharedService(className string) bool {
	if b.Container == nil {
		return true
	}
	className = strings.TrimPrefix(className, "\\")
	for _, def := range b.Container.Definitions {
		if strings.TrimPrefix(def.Class, "\\") == className {
			return def.Shared
		}
	}
	return false
}
