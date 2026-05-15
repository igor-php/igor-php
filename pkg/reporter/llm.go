package reporter

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/igor-php/igor-php/pkg/symbol"
)

// LLMOutput represents the strictly structured JSON format for LLMs.
type LLMOutput struct {
	Warnings []LLMWarning `json:"warnings"`
	Metadata LLMMetadata  `json:"metadata"`
}

// LLMWarning contains detailed context for a single detection.
type LLMWarning struct {
	FilePath   string     `json:"file_path"`
	Line       int        `json:"line"`
	Message    string     `json:"message"`
	Severity   string     `json:"severity"`
	Snippet    string     `json:"snippet"`
	ASTDetails string     `json:"ast_details"`
	RuleID     string     `json:"rule_id"`
	Context    LLMContext `json:"context"`
}

// LLMContext provides additional technical background for the warning.
type LLMContext struct {
	Dependencies []string `json:"dependencies"`
}

// LLMMetadata holds execution environment information.
type LLMMetadata struct {
	GeneratedAt time.Time `json:"generated_at"`
	IgorVersion string    `json:"igor_version"`
}

// LLMReporter implements the Reporter interface for JSON output.
type LLMReporter struct {
	IgorVersion string
	Warnings    []LLMWarning
}

// NewLLMReporter creates a new instance of LLMReporter.
func NewLLMReporter(version string) Reporter {
	return &LLMReporter{
		IgorVersion: version,
		Warnings:    []LLMWarning{},
	}
}

// PrintHeader is a no-op for LLMReporter.
func (r *LLMReporter) PrintHeader(count int) {}

// PrintFindings collects warnings to be exported later.
func (r *LLMReporter) PrintFindings(res symbol.AuditStatus, projectRoot string, isVendor bool) {
	for _, f := range res.Findings {
		w := LLMWarning{
			FilePath: res.FilePath,
			Line:     f.Line,
			Message:  f.Message,
			Severity: f.Severity,
			Snippet:  f.Code,
			// ASTDetails and Context will be populated in later phases
			RuleID: "STATE_MUTATION", // Default for now
		}
		r.Warnings = append(r.Warnings, w)
	}
}

// PrintSummary outputs the accumulated warnings as a JSON object.
func (r *LLMReporter) PrintSummary(results []symbol.AuditStatus, projectRoot string) bool {
	output := LLMOutput{
		Warnings: r.Warnings,
		Metadata: LLMMetadata{
			GeneratedAt: time.Now(),
			IgorVersion: r.IgorVersion,
		},
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Printf("Error generating LLM export: %v\n", err)
		return false
	}

	fmt.Println(string(data))

	// Determine success based on findings (match existing behavior)
	hasError := false
	for _, w := range r.Warnings {
		if w.Severity == "ERROR" || w.Severity == "KO" {
			hasError = true
			break
		}
	}

	return !hasError
}
