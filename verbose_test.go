package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerboseOutput(t *testing.T) {
	// 1. Setup mock Symfony project
	tmpDir, _ := os.MkdirTemp("", "igor_verbose_test")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	binDir := filepath.Join(tmpDir, "bin")
	_ = os.MkdirAll(binDir, 0755)

	// Create a mock console that returns a container with various service types
	mockConsoleContent := `<?php
if ($argv[1] === 'debug:container') {
    echo json_encode([
        'definitions' => [
            'app.safe_service' => [
                'class' => 'Symfony\\Component\\SafeService',
                'public' => true,
                'shared' => true
            ],
            'app.prototype_service' => [
                'class' => 'App\\PrototypeService',
                'public' => true,
                'shared' => false
            ],
            'app.normal_service' => [
                'class' => 'App\\NormalService',
                'public' => true,
                'shared' => true
            ]
        ],
        'aliases' => new stdClass()
    ]);
}
`
	_ = os.WriteFile(filepath.Join(binDir, "console"), []byte(mockConsoleContent), 0755)

	// 2. Capture Stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// 3. Run collectFiles logic via a minimal setup
	config := DefaultConfig()
	config.Verbose = true
	auditor := NewAuditor(config)
	
	// We need a Symfony bridge to simulate deep audit
	sb := NewSymfonyBridge(tmpDir, "bin/console")
	// Mock the container loading to avoid actual PHP execution issues in this test environment
	_ = sb.LoadContainer("prod")
	auditor.Symfony = sb

	// Call collectFiles (this will print to stdout because of Verbose=true)
	_ = collectFiles(tmpDir, config, auditor)

	// Restore Stdout
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = oldStdout

	output := buf.String()

	// 4. Assertions
	if !strings.Contains(output, "belongs to a safe namespace") {
		t.Errorf("Expected output to mention safe namespace skip, got:\n%s", output)
	}
	if !strings.Contains(output, "non-shared (prototype)") {
		t.Errorf("Expected output to mention prototype skip, got:\n%s", output)
	}
}
