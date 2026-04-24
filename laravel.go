package main

// LaravelBridge handles all interactions with the Laravel Framework.
type LaravelBridge struct {
	Root        string
	Config      Config
	ClassToFile map[string]string
}

// NewLaravelBridge creates a new bridge for a given project root.
func NewLaravelBridge(root string, config Config) *LaravelBridge {
	return &LaravelBridge{
		Root:        root,
		Config:      config,
		ClassToFile: make(map[string]string),
	}
}

// GetName returns the name of the framework.
func (b *LaravelBridge) GetName() string {
	return "Laravel"
}

// GetDefinitions returns all service definitions found in the Laravel container.
// This is a stub for Phase 1.
func (b *LaravelBridge) GetDefinitions() map[string]ServiceDefinition {
	return make(map[string]ServiceDefinition)
}

// GetClassToFileMap returns the class-to-file mapping for Laravel.
func (b *LaravelBridge) GetClassToFileMap() map[string]string {
	return b.ClassToFile
}

// IsSharedService checks if a FQCN is a shared service (singleton) in Laravel.
// For now, we assume all detected services are shared in the context of Octane.
func (b *LaravelBridge) IsSharedService(className string) bool {
	return true
}
