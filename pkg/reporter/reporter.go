package reporter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/igor-php/igor-php/pkg/symbol"
)

// Reporter handles the output formatting of audit results.
type Reporter interface {
	PrintHeader(count int)
	PrintProjectHeader()
	PrintVendorHeader()
	PrintFindings(res symbol.AuditStatus, projectRoot string, isVendor bool)
	PrintSummary(results []symbol.AuditStatus, projectRoot string) bool
}

// FoldedNotifier is implemented by the reporters that fold the findings of code that never runs
// during a request: they print how many were left out instead.
type FoldedNotifier interface {
	PrintFolded(count int, categories []string)
}

// JSONReporter implements Reporter for standard JSON output.
type JSONReporter struct {
	StartTime time.Time
	Findings  []symbol.AuditStatus
}

// NewJSONReporter creates a new JSON reporter.
func NewJSONReporter() Reporter {
	return &JSONReporter{
		StartTime: time.Now(),
		Findings:  []symbol.AuditStatus{},
	}
}

func (r *JSONReporter) PrintHeader(_ int)   {}
func (r *JSONReporter) PrintProjectHeader() {}
func (r *JSONReporter) PrintVendorHeader()  {}

func (r *JSONReporter) PrintFindings(res symbol.AuditStatus, _ string, _ bool) {
	if len(res.Findings) > 0 {
		r.Findings = append(r.Findings, res)
	}
}

func (r *JSONReporter) PrintSummary(_ []symbol.AuditStatus, _ string) bool {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(r.Findings); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating JSON export: %v\n", err)
		return false
	}
	return true
}

// CLIReporter implements Reporter for terminal output.
type CLIReporter struct {
	StartTime time.Time
	IsGitHub  bool
}

// NewReporter creates a new CLI reporter.
func NewReporter() Reporter {
	return &CLIReporter{
		StartTime: time.Now(),
		IsGitHub:  os.Getenv("GITHUB_ACTIONS") == "true",
	}
}

// PrintHeader prints the initial message.
func (r *CLIReporter) PrintHeader(count int) {
	fmt.Printf("🧟 Igor is auditing %d unique shared service files for you, Master...\n\n", count)
}

// PrintProjectHeader prints the project services section header.
func (r *CLIReporter) PrintProjectHeader() {
	fmt.Println("\n\033[34m--- 📂 PROJECT SERVICES ---\033[0m")
}

// PrintVendorHeader prints the vendor services section header.
func (r *CLIReporter) PrintVendorHeader() {
	fmt.Println("\n\033[33m--- 📦 VENDOR SERVICES (THIRD-PARTY) ---\033[0m")
}

// PrintFindings displays detailed issues for a given service.
func (r *CLIReporter) PrintFindings(res symbol.AuditStatus, projectRoot string, isVendor bool) {
	if len(res.Findings) == 0 {
		return
	}

	displayPath := res.FilePath
	// Use relative path for cleaner output if possible
	relPath := res.FilePath
	if rel, found := strings.CutPrefix(res.FilePath, projectRoot); found && rel != "" {
		relPath = strings.TrimPrefix(rel, "/")
		displayPath = relPath
	} else {
		// Fallback: try to get relative path from current working directory
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, res.FilePath); err == nil {
				relPath = rel
			}
		}
	}

	fmt.Printf("\n📂 \033[1m%s\033[0m\n", displayPath)
	if res.ServiceID != "N/A" {
		fmt.Printf("   \033[90mService: %s\033[0m\n", res.ServiceID)
	}

	findings := make([]symbol.Finding, len(res.Findings))
	copy(findings, res.Findings)
	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].Reachability == "HIGH" && findings[j].Reachability != "HIGH"
	})

	sourceIndicator := "\033[34m[PROJECT]\033[0m"
	if isVendor {
		sourceIndicator = "\033[33m[VENDOR]\033[0m"
	}

	// Keyed caches and the properties reset() forgets are listed together after the other findings
	var caches []symbol.Finding
	notReset := make(map[string][]symbol.Finding)
	var notResetClasses []string
	for _, f := range findings {
		switch {
		case f.Ignored:
			r.printFinding(f, sourceIndicator)
			continue
		case f.Category == symbol.CategoryCache:
			caches = append(caches, f)
		case f.Category == symbol.CategoryNotReset:
			if _, seen := notReset[f.ContextClass]; !seen {
				notResetClasses = append(notResetClasses, f.ContextClass)
			}
			notReset[f.ContextClass] = append(notReset[f.ContextClass], f)
		default:
			r.printFinding(f, sourceIndicator)
		}
		r.annotate(f, relPath, isVendor)
	}
	for _, class := range notResetClasses {
		printNotResetGroup(class, notReset[class], sourceIndicator)
	}
	if len(caches) > 0 {
		printCaches(caches, sourceIndicator)
	}
}

// printFinding prints one finding with its code line and hint.
func (r *CLIReporter) printFinding(f symbol.Finding, sourceIndicator string) {
	// Baseline-exempt findings (--include-ignored) are shown dimmed and never annotated
	if f.Ignored {
		fmt.Printf("  %s \033[90m[IGNORED] %s\033[0m\n", sourceIndicator, f.Message)
		fmt.Printf("  \033[90m%d | %s\033[0m\n", f.Line, strings.TrimSpace(f.Code))
		if f.IgnoreReason != "" {
			fmt.Printf("  \033[90m🛡️  Baseline reason: %s\033[0m\n", f.IgnoreReason)
		}
		return
	}

	fmt.Printf("  %s%s %s%s\033[0m\n", sourceIndicator, findingTags(f), severityColor(f), f.Message)
	fmt.Printf("  \033[90m%d | %s\033[0m\n", f.Line, strings.TrimSpace(f.Code))
	if f.Remediation != "" {
		fmt.Printf("  %s💡 Hint: %s\033[0m\n", "\033[36m", f.Remediation)
	}
}

// printNotResetGroup prints in one block the properties of a resettable class that reset() forgets.
func printNotResetGroup(class string, findings []symbol.Finding, sourceIndicator string) {
	noun := "properties"
	if len(findings) == 1 {
		noun = "property"
	}
	// The class carries the tags its properties share; when they differ, each property shows its own
	shared := findingTags(findings[0])
	for _, f := range findings[1:] {
		if findingTags(f) != shared {
			shared = ""
			break
		}
	}
	fmt.Printf("  %s%s \033[33m⚠️  %s (resettable) — %d %s mutated but not reset in reset():\033[0m\n",
		sourceIndicator, shared, class, len(findings), noun)
	items := make([]string, 0, len(findings))
	for _, f := range findings {
		item := fmt.Sprintf("%s (line %d)", f.Property, f.Line)
		if f.ContextMethod != "" {
			item = fmt.Sprintf("%s (%s, line %d)", f.Property, f.ContextMethod, f.Line)
		}
		if shared == "" {
			item += findingTags(f)
		}
		items = append(items, item)
	}
	for _, line := range wrapList(items, 96) {
		fmt.Printf("      %s\n", line)
	}
	fmt.Printf("  \033[36m💡 Hint: Clear these properties in the reset() method.\033[0m\n")
}

// printCaches prints the keyed cache writes with their key, so the reviewer can tell whether one derives from the request.
func printCaches(caches []symbol.Finding, sourceIndicator string) {
	for _, f := range caches {
		fmt.Printf("  %s%s \033[36m🗄️  Cache\033[0m  %s::$%s  key: %s\n", sourceIndicator, findingTags(f), f.ContextClass, f.Property, f.CacheKey)
		fmt.Printf("  \033[90m%d | %s\033[0m\n", f.Line, strings.TrimSpace(f.Code))
	}
	fmt.Printf("  \033[36m💡 Hint: Keyed caches are usually bounded (keyed by class, format or route). Check that no key derives from the request (URI, user, query): such a cache grows with every request.\033[0m\n")
}

// annotate prints the GitHub Actions annotation of a finding, one per finding even when the text output groups them.
func (r *CLIReporter) annotate(f symbol.Finding, relPath string, isVendor bool) {
	if !r.IsGitHub {
		return
	}
	severity := "error"
	if f.Severity == "WARNING" {
		severity = "warning"
	}
	msg := fmt.Sprintf("[Igor] %s", f.Message)
	if f.Remediation != "" {
		msg += fmt.Sprintf(" %%0A 💡 Hint: %s", f.Remediation)
	}
	if isVendor {
		msg += " %0A 💡 Hint: This is third-party code. If you can't fix it, consider setting a 'max_requests' limit in your Worker configuration to mitigate memory leaks. Determine whether the issue is simply a memory leak or the result of a critical data exchange between two requests."
	} else {
		msg += " %0A 💡 Hint: Since this is your code, you should refactor this service to be stateless, or clear its state between requests (Symfony ResetInterface, or a reset() called by an Octane request listener on Laravel)."
	}
	// Format: ::error file={name},line={line},title={title}::{message}
	title := ""
	if f.Rule != "" {
		title = ",title=Igor " + f.Rule
	}
	fmt.Printf("::%s file=%s,line=%d%s::%s\n", severity, relPath, f.Line, title, msg)
}

func severityColor(f symbol.Finding) string {
	if f.Severity == "WARNING" {
		return "\033[33m" // Yellow for Warning
	}
	return "\033[31m" // Red for Error
}

// findingTags returns the reachability and out-of-request tags shown before a finding.
func findingTags(f symbol.Finding) string {
	tags := ""
	switch f.Reachability {
	case "HIGH":
		tags = " \033[1;31m[HIGH]\033[0m"
	case "INFO":
		tags = " \033[90m[INFO]\033[0m"
	}
	// Only listed with --show-all: the code never runs during a request
	if f.OutOfRequest != "" {
		tags += fmt.Sprintf(" \033[90m[OUT OF REQUEST: %s]\033[0m", f.OutOfRequest)
	}
	return tags
}

// wrapList joins items with commas into lines of at most width characters.
func wrapList(items []string, width int) []string {
	var lines []string
	current := ""
	for i, item := range items {
		if i < len(items)-1 {
			item += ","
		}
		if current != "" && len(current)+1+len(item) > width {
			lines = append(lines, current)
			current = item
			continue
		}
		if current != "" {
			current += " "
		}
		current += item
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// PrintFolded prints how many findings were left out because their code never runs during a request.
func (r *CLIReporter) PrintFolded(count int, categories []string) {
	noun := "findings"
	if count == 1 {
		noun = "finding"
	}
	fmt.Printf("\nℹ️  %d %s in code that does not run during a request (%s) — use --show-all to list them\n",
		count, noun, strings.Join(categories, ", "))
}

// PrintSummary displays the final audit statistics.
func (r *CLIReporter) PrintSummary(results []symbol.AuditStatus, projectRoot string) bool {
	totalOK := 0
	projKO, projWarn := 0, 0
	vendKO, vendWarn := 0, 0

	for _, res := range results {
		isVendor := res.IsVendor(projectRoot)
		switch res.Status {
		case "✅ OK":
			totalOK++
		case "❌ KO":
			if isVendor {
				vendKO++
			} else {
				projKO++
			}
		case "⚠️  WARN":
			if isVendor {
				vendWarn++
			} else {
				projWarn++
			}
		}
	}

	totalKO := projKO + vendKO
	totalWarn := projWarn + vendWarn

	fmt.Printf("\n--- 🏁 DEEP AUDIT COMPLETE ---")
	fmt.Printf("\nTotal unique service files: %d", totalOK+totalKO+totalWarn)
	fmt.Printf("\n✅ OK (Stateless):           %d", totalOK)
	fmt.Printf("\n❌ KO (Dangerous State):     %d (Project: %d, Vendor: %d)", totalKO, projKO, vendKO)
	fmt.Printf("\n⚠️  WARN (Review reset):      %d (Project: %d, Vendor: %d)", totalWarn, projWarn, vendWarn)
	fmt.Printf("\nTime taken: %v\n", time.Since(r.StartTime))

	// Detailed Recommendations
	if totalKO > 0 || totalWarn > 0 {
		fmt.Println("\n\033[1m💡 RECOMMENDATIONS:\033[0m")

		if projKO > 0 || projWarn > 0 {
			fmt.Println("  \033[34m[PROJECT]\033[0m Since this is your code, you should refactor these services to be stateless")
			fmt.Println("            or clear their state between requests (Symfony ResetInterface, or a reset() called")
			fmt.Println("            by an Octane request listener on Laravel).")
		}

		if vendKO > 0 || vendWarn > 0 {
			fmt.Println("  \033[33m[VENDOR]\033[0m  This is third-party code. If you can't fix it, consider setting a 'max_requests' limit")
			fmt.Println("            in your Worker configuration (e.g. FrankenPHP) to mitigate memory leaks.")
			fmt.Println("            Determine if the issue is a simple leak or a critical data exchange between requests.")
		}
	}

	if totalKO > 0 {
		fmt.Println("\n\033[31m⚠️  DANGER: Your application or its vendors contain services with shared state.")
		fmt.Println("These services will leak data between requests in Worker Mode.\033[0m")
		return false
	}

	if totalWarn > 0 {
		fmt.Println("\n\033[33m⚠️  CAUTION: Your application contains possible state mutations (Warnings).")
		fmt.Println("Please review these services to ensure they are compatible with Worker Mode.\033[0m")
		return true
	}

	fmt.Println("\n\033[32m✨ CONGRATULATIONS: Your application and all its dependencies are compatible with Worker Mode!\033[0m")
	return true
}
