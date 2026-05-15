package reporter

import (
	"testing"

	"github.com/igor-php/igor-php/pkg/symbol"
)

func TestLLMReporter(t *testing.T) {
	rep := NewLLMReporter("dev")

	// Test PrintHeader (no-op)
	rep.PrintHeader(10)

	// Test PrintFindings
	status := symbol.AuditStatus{
		FilePath: "src/Service/StatefulService.php",
		Findings: []symbol.Finding{
			{
				Line:     15,
				Message:  "Mutation of property $counter detected",
				Severity: "WARNING",
				Code:     "$this->counter++;",
			},
		},
	}
	rep.PrintFindings(status, "/root", false)

	llmRep := rep.(*LLMReporter)
	if len(llmRep.Warnings) != 1 {
		t.Errorf("Expected 1 warning buffered, got %d", len(llmRep.Warnings))
	}

	if llmRep.Warnings[0].Snippet != "$this->counter++;" {
		t.Errorf("Expected snippet '$this->counter++', got '%s'", llmRep.Warnings[0].Snippet)
	}

	// Test PrintSummary (should return true since it's only a warning)
	success := rep.PrintSummary([]symbol.AuditStatus{status}, "/root")
	if !success {
		t.Error("Expected success=true for warnings-only report")
	}
}
