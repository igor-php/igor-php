package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSymfonyIntegration(t *testing.T) {
	// 1. Create a temporary Symfony project structure
	tmpDir, err := os.MkdirTemp("", "mock_symfony_deep")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	binDir := filepath.Join(tmpDir, "bin")
	vendorDir := filepath.Join(tmpDir, "vendor")
	srcDir := filepath.Join(tmpDir, "src", "Service")
	os.MkdirAll(binDir, 0755)
	os.MkdirAll(vendorDir, 0755)
	os.MkdirAll(srcDir, 0755)

	// 2. Create a dummy service file
	serviceContent := `<?php
namespace App\Service;
class MyService {
    private $state;
    public function set($v) { $this->state = $v; }
}
`
	servicePath := filepath.Join(srcDir, "MyService.php")
	err = os.WriteFile(servicePath, []byte(serviceContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// 3. Create a mini vendor/autoload.php that registers our class
	autoloaderContent := `<?php
spl_autoload_register(function ($class) {
    if ($class === 'App\\Service\\MyService') {
        require_once '` + servicePath + `';
    }
});
`
	err = os.WriteFile(filepath.Join(vendorDir, "autoload.php"), []byte(autoloaderContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// 4. Create a mock bin/console
	mockConsoleContent := `<?php
if ($argv[1] === 'debug:container') {
    echo json_encode([
        'definitions' => [
            'app.my_service' => [
                'class' => 'App\\Service\\MyService',
                'public' => true,
                'shared' => true
            ]
        ],
        'aliases' => new stdClass()
    ]);
}
`
	err = os.WriteFile(filepath.Join(binDir, "console"), []byte(mockConsoleContent), 0755)
	if err != nil {
		t.Fatal(err)
	}

	// 5. Test Full Audit Pipeline
	t.Run("Bridge should load container and LOCATE files via Reflection", func(t *testing.T) {
		bridge := NewSymfonyBridge(tmpDir)
		err := bridge.LoadContainer()
		if err != nil {
			t.Fatalf("LoadContainer failed: %v", err)
		}

		if bridge.Container == nil {
			t.Fatal("Expected container to be loaded")
		}

		// Verify reflection mapping
		filePath, found := bridge.ClassToFile["App\\Service\\MyService"]
		if !found {
			t.Fatal("Expected App\\Service\\MyService to be located via reflection")
		}
		
		// Evaluate symlinks to handle /private/var on macOS
		realServicePath, _ := filepath.EvalSymlinks(servicePath)
		realLocatedPath, _ := filepath.EvalSymlinks(filePath)

		if realLocatedPath != realServicePath {
			t.Errorf("Expected path %s, got %s", realServicePath, realLocatedPath)
		}
	})

	t.Run("Auditor should correctly audit the mocked service", func(t *testing.T) {
		cfg := Config{}
		auditor := NewAuditor(cfg)
		
		findings, err := auditor.Audit(servicePath)
		if err != nil {
			t.Fatalf("Audit failed: %v", err)
		}

		if len(findings) == 0 {
			t.Error("Expected 1 finding for stateful service, got 0")
		}
	})
}
