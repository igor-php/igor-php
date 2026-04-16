package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestGitHubAnnotations(t *testing.T) {
	// 1. Force GitHub Actions environment
	_ = os.Setenv("GITHUB_ACTIONS", "true")
	defer func() { _ = os.Unsetenv("GITHUB_ACTIONS") }()

	// 2. Setup Reporter and a dummy result
	reporter := NewReporter()
	if !reporter.isGitHub {
		t.Fatal("Reporter should detect GITHUB_ACTIONS=true")
	}

	res := AuditStatus{
		FilePath:  "src/Service/MyService.php",
		ServiceID: "app.my_service",
		Findings: []Finding{
			{
				Message:     "Mutation of state 'cache'",
				Severity:    "ERROR",
				Line:        42,
				Code:        "$this->cache = [];",
				Remediation: "Reset it.",
			},
		},
	}

	// 3. Capture Stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	reporter.PrintFindings(res, "/tmp", false) // dummy project root, not a vendor

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = oldStdout

	output := buf.String()

	// 4. Assertions
	expectedAnnotation := "::error file=src/Service/MyService.php,line=42::[Igor] Mutation of state 'cache' %0A 💡 Hint: Reset it."
	if !strings.Contains(output, expectedAnnotation) {
		t.Errorf("Expected annotation not found in output.\nGot: %s\nExpected: %s", output, expectedAnnotation)
	}
}
