package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditFixtures(t *testing.T) {
	cfg := Config{}
	auditor := NewAuditor(cfg)

	tests := []struct {
		name           string
		fixture        string
		expectedErrors int
		contains       string
	}{
		{
			name:           "Clean code should have 0 errors",
			fixture:        "clean_code.php",
			expectedErrors: 0,
		},
		{
			name:           "Simple state mutation",
			fixture:        "state_mutation.php",
			expectedErrors: 5,
			contains:       "Mutation of state",
		},
		{
			name:           "Array state mutation",
			fixture:        "array_mutation.php",
			expectedErrors: 4,
			contains:       "Mutation of state",
		},
		{
			name:           "Execution terminators",
			fixture:        "terminators.php",
			expectedErrors: 2,
			contains:       "forbidden",
		},
		{
			name:           "ResetInterface partial cleanup",
			fixture:        "reset_check.php",
			expectedErrors: 1, // Only IncompleteResetService should fail/warn
			contains:       "not reset in reset()",
		},
		{
			name:           "ResetInterface partial cleanup (3 props, 2 reset)",
			fixture:        "reset_incomplete.php",
			expectedErrors: 1,
			contains:       "Property 'prop3' of IncompleteService is mutated but not reset",
		},
		{
		        name:           "Security risks (superglobals & static vars)",
		        fixture:        "security_risks.php",
		        expectedErrors: 9, // 8 superglobals + 1 static var
		        contains:       "$request->query",
		},		{
			name:           "Complex mutations (nested & dynamic)",
			fixture:        "complex_mutations.php",
			expectedErrors: 2, // Nested + Dynamic (Reference is harder to detect without data flow)
			contains:       "Mutation of state",
		},
		{
			name:           "Ignore annotation (@igor-ignore)",
			fixture:        "ignore_annotation.php",
			expectedErrors: 1,
			contains:       "Mutation of state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join("tests", "fixtures", tt.fixture)
			findings, err := auditor.Audit(path)
			if err != nil {
				t.Fatalf("Failed to audit %s: %v", path, err)
			}

			if len(findings) != tt.expectedErrors {
				t.Errorf("Expected %d findings, got %d", tt.expectedErrors, len(findings))
				for _, f := range findings {
					t.Logf("- %s (Line %d)", f.Message, f.Line)
				}
			}

			if tt.contains != "" && len(findings) > 0 {
			        found := false
			        for _, f := range findings {
			                if strings.Contains(f.Message, tt.contains) || strings.Contains(f.Remediation, tt.contains) {
			                        found = true
			                        break
			                }
			        }
			        if !found {
			                t.Errorf("Expected findings (Message or Remediation) to contain %q", tt.contains)
			        }
			}		})
	}
}
