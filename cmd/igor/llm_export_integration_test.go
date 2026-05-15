package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/pkg/reporter"
)

func TestLLMExportE2E(t *testing.T) {
	// 1. Setup a dummy project
	tmpDir, err := os.MkdirTemp("", "igor-llm-e2e-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	serviceContent := `<?php
class StatefulService {
    private $counter = 0;
    public function increment() {
        $this->counter++;
    }
}`
	servicePath := filepath.Join(tmpDir, "StatefulService.php")
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Run igor-php with --output=llm
	// We use go run . to run the current project
	cmd := exec.Command("go", "run", ".", "--output=llm", tmpDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Output: %s", string(output))
		// We expect an error exit code if findings are errors (which they are by default)
	}

	outStr := string(output)
	// Find the start of JSON (skip any pre-header prints if any)
	jsonStart := strings.Index(outStr, "{")
	if jsonStart == -1 {
		t.Fatalf("Could not find JSON output. Output was:\n%s", outStr)
	}
	jsonEnd := strings.LastIndex(outStr, "}")
	if jsonEnd == -1 || jsonEnd < jsonStart {
		t.Fatalf("Could not find end of JSON output. Output was:\n%s", outStr)
	}
	jsonPart := outStr[jsonStart : jsonEnd+1]

	// 3. Parse and Verify JSON
	var export reporter.LLMOutput
	if err := json.Unmarshal([]byte(jsonPart), &export); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput was:\n%s", err, jsonPart)
	}

	if len(export.Warnings) == 0 {
		t.Fatal("Expected at least one warning in LLM export")
	}

	found := false
	for _, w := range export.Warnings {
		if strings.Contains(w.Message, "Mutation of state") {
			found = true
			if w.Snippet == "" {
				t.Error("Expected non-empty snippet in LLM export")
			}
			if w.ASTDetails == "" {
				t.Error("Expected non-empty AST details in LLM export")
			}
			break
		}
	}

	if !found {
		t.Error("Expected 'Mutation of state' warning not found in LLM export")
	}
}
