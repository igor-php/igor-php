package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestVersionInjection(t *testing.T) {
	testVersion := "v99.99.99-test"

	// Run go run with ldflags to inject the version
	cmd := exec.Command("go", "run", "-ldflags", "-X main.Version="+testVersion, ".", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run igor-php --version: %v\nOutput: %s", err, string(output))
	}

	outStr := string(output)
	if !strings.Contains(outStr, testVersion) {
		t.Errorf("Expected version %s in output, got: %s", testVersion, outStr)
	}
}

func TestHelpVersionDisplay(t *testing.T) {
	testVersion := "v88.88.88-help-test"

	// Run go run with ldflags and check help output
	cmd := exec.Command("go", "run", "-ldflags", "-X main.Version="+testVersion, ".", "--help")
	output, _ := cmd.CombinedOutput()

	outStr := string(output)
	if !strings.Contains(outStr, testVersion) {
		t.Errorf("Expected version %s in help output, got: %s", testVersion, outStr)
	}

	if !strings.Contains(outStr, "The faithful assistant") {
		t.Error("Help output missing project description")
	}
}
