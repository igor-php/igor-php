package main

import (
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/reporter"
	"github.com/igor-php/igor-php/pkg/symbol"
)

func setupReporter(cfg config.Config) reporter.Reporter {
	switch cfg.OutputFormat {
	case "llm":
		return reporter.NewLLMReporter(Version)
	case "json":
		return reporter.NewJSONReporter()
	default:
		return reporter.NewReporter()
	}
}

// reportAllFindings prints the project findings, then the vendor ones. With fold set, the findings
// of code that never runs during a request are left out and counted on one line instead.
func reportAllFindings(rep reporter.Reporter, results []symbol.AuditStatus, rootPath string, fold bool) {
	shown := results
	if fold {
		shown = make([]symbol.AuditStatus, len(results))
		for i, res := range results {
			res.Findings = inRequestFindings(res.Findings)
			shown[i] = res
		}
	}

	// Report Project Results first
	hasProjectFindings := false
	for _, res := range shown {
		isVendor := res.IsVendor(rootPath)
		if !isVendor && len(res.Findings) > 0 {
			if !hasProjectFindings {
				rep.PrintProjectHeader()
				hasProjectFindings = true
			}
			rep.PrintFindings(res, rootPath, isVendor)
		}
	}

	// Report Vendor Results second
	hasVendorFindings := false
	for _, res := range shown {
		isVendor := res.IsVendor(rootPath)
		if isVendor && len(res.Findings) > 0 {
			if !hasVendorFindings {
				rep.PrintVendorHeader()
				hasVendorFindings = true
			}
			rep.PrintFindings(res, rootPath, isVendor)
		}
	}

	if !fold {
		return
	}
	if notifier, ok := rep.(reporter.FoldedNotifier); ok {
		if count, categories := foldedSummary(results); count > 0 {
			notifier.PrintFolded(count, categories)
		}
	}
}
