package main

import (
	"strings"
)

// FrameworkBridge represents a common interface for different PHP frameworks (Symfony, Laravel, etc.)
// to provide service definitions and class-to-file mappings for the auditor.
type FrameworkBridge interface {
	// GetName returns the name of the framework.
	GetName() string

	// GetDefinitions returns all service definitions found in the framework's container.
	GetDefinitions() map[string]ServiceDefinition

	// GetClassToFileMap returns a mapping of FQCN to their absolute file paths.
	GetClassToFileMap() map[string]string

	// IsSharedService returns true if the given class name is defined as a shared service/singleton.
	IsSharedService(className string) bool
}

// ServiceDefinition provides a framework-agnostic view of a service.
type ServiceDefinition struct {
	ID     string
	Class  string
	Shared bool
}

// NormalizeClassName ensures the class name is consistently formatted (no leading backslash).
func NormalizeClassName(className string) string {
	return strings.TrimLeft(className, "\\")
}
