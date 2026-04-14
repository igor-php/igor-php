package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Reporter handles the output formatting of audit results.
type Reporter struct {
	StartTime time.Time
	isGitHub  bool
}

// NewReporter creates a new reporter.
func NewReporter() *Reporter {
	return &Reporter{
		StartTime: time.Now(),
		isGitHub:  os.Getenv("GITHUB_ACTIONS") == "true",
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
	relPath := res.FilePath
	if rel, found := strings.CutPrefix(res.FilePath, projectRoot); found {
		relPath = strings.TrimPrefix(rel, "/")
		displayPath = relPath
	}

	fmt.Printf("\n📂 \033[1m%s\033[0m\n", displayPath)
	if res.ServiceID != "N/A" {
		fmt.Printf("   \033[90mService: %s\033[0m\n", res.ServiceID)
	}

	for _, f := range res.Findings {
		severity := "error"
		color := "\033[31m" // Red for Error
		if f.Severity == "WARNING" {
			severity = "warning"
			color = "\033[33m" // Yellow for Warning
		}

		// Standard CLI output
		fmt.Printf("  %s%s\033[0m\n", color, f.Message)
		fmt.Printf("  \033[90m%d | %s\033[0m\n", f.Line, strings.TrimSpace(f.Code))
		if f.Remediation != "" {
			fmt.Printf("  %s💡 Hint: %s\033[0m\n", "\033[36m", f.Remediation)
		}

		// GitHub Action Annotation
		if r.isGitHub {
			msg := fmt.Sprintf("[Igor] %s", f.Message)
			if f.Remediation != "" {
				msg += fmt.Sprintf(" %%0A 💡 Hint: %s", f.Remediation)
			}
			// Format: ::error file={name},line={line},col={col}::{message}
			fmt.Printf("::%s file=%s,line=%d::%s\n", severity, relPath, f.Line, msg)
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
