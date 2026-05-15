package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVendorExclusionBug(t *testing.T) {
	// Setup a mock project
	tmpDir, err := os.MkdirTemp("", "repro_vendor_bug")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	binDir := filepath.Join(tmpDir, "bin")
	vendorDir := filepath.Join(tmpDir, "vendor", "some-pkg")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Service in vendor
	servicePath := filepath.Join(vendorDir, "VendorService.php")
	serviceContent := `<?php
namespace SomePkg;
class VendorService {
    private $state;
    public function set($v) { $this->state = $v; }
}
`
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Mock console to return this service as shared
	mockConsoleContent := `<?php
if ($argv[1] === 'debug:container') {
    echo json_encode([
        'definitions' => [
            'some_pkg.vendor_service' => [
                'class' => 'SomePkg\\VendorService',
                'public' => true,
                'shared' => true
            ]
        ],
        'aliases' => new stdClass()
    ]);
}
`
	if err := os.WriteFile(filepath.Join(binDir, "console"), []byte(mockConsoleContent), 0755); err != nil {
		t.Fatal(err)
	}

	// Config with vendor excluded
	config := Config{
		Exclude:     []string{"vendor"},
		ConsolePath: "bin/console",
		NoAgent:     true,
	}

	auditor := NewAuditor(config)
	bridge := NewSymfonyBridge(tmpDir, config.ConsolePath, config)

	// Manually inject the ClassToFile mapping since reflection won't work easily here without proper autoloader
	bridge.Container = &SymfonyContainer{
		Definitions: map[string]SymfonyService{
			"some_pkg.vendor_service": {
				Class:  "SomePkg\\VendorService",
				Public: true,
				Shared: true,
			},
		},
	}
	bridge.ClassToFile = map[string]string{
		"SomePkg\\VendorService": servicePath,
	}
	auditor.Symfony = bridge

	// Now run collectFiles
	auditList := collectFiles(tmpDir, config, auditor)

	// We expect auditList to NOT contain VendorService because vendor is excluded
	found := false
	for _, status := range auditList {
		if status.ServiceID == "some_pkg.vendor_service" {
			found = true
			break
		}
	}

	if found {
		t.Errorf("Expected some_pkg.vendor_service to be excluded because its path is in vendor/, but it was found in audit list")
	}

	t.Run("Should handle trailing slashes in exclude patterns", func(t *testing.T) {
		configSlash := Config{
			Exclude:     []string{"vendor/"},
			ConsolePath: "bin/console",
			NoAgent:     true,
		}
		auditListSlash := collectFiles(tmpDir, configSlash, auditor)
		foundSlash := false
		for _, status := range auditListSlash {
			if status.ServiceID == "some_pkg.vendor_service" {
				foundSlash = true
				break
			}
		}
		if foundSlash {
			t.Errorf("Expected some_pkg.vendor_service to be excluded with 'vendor/' pattern, but it was found")
		}
	})
}
