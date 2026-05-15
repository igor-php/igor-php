package main

import (
	"bytes"
	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
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

	// 2. Capture Stdout and Stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w

	// 3. Run collectFiles logic via a minimal setup
	cfg := config.DefaultConfig()
	cfg.Verbose = true
	aud := auditor.NewAuditor(cfg)

	// We need a Symfony bridge to simulate deep audit
	sb := auditor.NewSymfonyBridge(tmpDir, "bin/console", config.Config{NoAgent: true})
	// Mock the container loading to avoid actual PHP execution issues in this test environment
	_ = sb.LoadContainer("prod")
	aud.Symfony = sb

	// Call collectFiles (this will print to stderr because of Verbose=true)
	_ = collectFiles(tmpDir, cfg, aud)

	// Restore Stdout and Stderr
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	output := buf.String()

	// 4. Assertions
	if !strings.Contains(output, "belongs to a safe namespace") {
		t.Errorf("Expected output to mention safe namespace skip, got:\n%s", output)
	}
	if !strings.Contains(output, "non-shared (prototype)") {
		t.Errorf("Expected output to mention prototype skip, got:\n%s", output)
	}
}
