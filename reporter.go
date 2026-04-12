package main

import (
	"fmt"
	"strings"
	"time"
)

// Reporter handles the output formatting of audit results.
type Reporter struct {
	StartTime time.Time
}

// NewReporter creates a new reporter.
func NewReporter() *Reporter {
	return &Reporter{
		StartTime: time.Now(),
	}
}

// PrintHeader prints the initial message.
func (r *Reporter) PrintHeader(count int) {
	fmt.Printf("🧟 Igor is auditing %d unique shared service files for you, Master...\n\n", count)
}

// PrintFindings displays detailed issues for a given service.
func (r *Reporter) PrintFindings(res AuditStatus, projectRoot string) {
	if len(res.Findings) == 0 {
		return
	}

	displayPath := res.FilePath
	// Use relative path for cleaner output if possible
	if rel, err := strings.CutPrefix(res.FilePath, projectRoot); err {
		displayPath = strings.TrimPrefix(rel, "/")
	}

	fmt.Printf("\n📂 \033[1m%s\033[0m\n", displayPath)
	if res.ServiceID != "N/A" {
		fmt.Printf("   \033[90mService: %s\033[0m\n", res.ServiceID)
	}

	for _, f := range res.Findings {
		color := "\033[31m" // Red for Error
		if f.Severity == "WARNING" {
			color = "\033[33m" // Yellow for Warning
		}
		fmt.Printf("  %s%s\033[0m\n", color, f.Message)
		fmt.Printf("  \033[90m%d | %s\033[0m\n", f.Line, strings.TrimSpace(f.Code))
		if f.Remediation != "" {
			fmt.Printf("  \033[36m💡 Hint: %s\033[0m\n", f.Remediation)
		}
	}
}

// PrintSummary displays the final audit statistics.
func (r *Reporter) PrintSummary(results []AuditStatus) bool {
	totalOK, totalKO, totalWarn := 0, 0, 0
	for _, res := range results {
		switch res.Status {
		case "✅ OK":
			totalOK++
		case "❌ KO":
			totalKO++
		case "⚠️  WARN":
			totalWarn++
		}
	}

	fmt.Printf("\n--- 🏁 DEEP AUDIT COMPLETE ---")
	fmt.Printf("\nTotal unique service files: %d", totalOK+totalKO+totalWarn)
	fmt.Printf("\n✅ OK (Stateless):           %d", totalOK)
	fmt.Printf("\n❌ KO (Dangerous State):     %d", totalKO)
	fmt.Printf("\n⚠️  WARN (Review reset):      %d", totalWarn)
	fmt.Printf("\nTime taken: %v\n", time.Since(r.StartTime))

	if totalKO > 0 {
		fmt.Println("\n\033[31m⚠️  DANGER: Your application or its vendors contain services with shared state.")
		fmt.Println("These services will leak data between requests in Worker Mode.\033[0m")
		return false
	}

	fmt.Println("\n\033[32m✨ CONGRATULATIONS: Your application and all its dependencies are compatible with Worker Mode!\033[0m")
	return true
}
