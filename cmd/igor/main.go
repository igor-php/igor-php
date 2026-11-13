package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/igor-php/igor-php/internal/analyzer"
	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/reporter"
	"github.com/igor-php/igor-php/pkg/symbol"
)

var Version = "dev"
var binName = "igor"

func main() {
	cfg, rootPath, shouldExit, err := parseFlagsAndInit(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
		os.Exit(1)
	}
	if shouldExit {
		return
	}

	rep := setupReporter(cfg)
	results, baseline, err := runAudit(&cfg, rootPath, rep)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
		os.Exit(1)
	}

	// 7a. Handle Baseline Checking
	if cfg.CheckBaseline {
		staleEntries := config.IdentifyStaleEntries(baseline, results, rootPath)
		if len(staleEntries) > 0 {
			fmt.Fprintf(os.Stderr, "❌ Baseline check failed: %d stale entries detected in baseline (%s):\n", len(staleEntries), cfg.BaselinePath)
			for _, entry := range staleEntries {
				fmt.Fprintf(os.Stderr, "  - %s: %s\n", entry.FilePath, entry.Message)
			}
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "✨ Baseline check passed! The baseline (%s) is perfectly clean and up to date.\n", cfg.BaselinePath)
		return
	}

	// 7b. Handle Baseline Pruning
	if cfg.PruneBaseline {
		staleEntries := config.IdentifyStaleEntries(baseline, results, rootPath)
		if len(staleEntries) == 0 {
			fmt.Fprintf(os.Stderr, "✨ No stale entries found. The baseline (%s) is already up to date.\n", cfg.BaselinePath)
			return
		}

		fmt.Fprintf(os.Stderr, "⚙️  Pruning %d stale entries from baseline (%s)...\n", len(staleEntries), cfg.BaselinePath)
		prunedBaseline := config.PruneBaseline(baseline, staleEntries)

		baselineFile := cfg.BaselinePath
		if !filepath.IsAbs(baselineFile) {
			baselineFile = filepath.Join(rootPath, baselineFile)
		}

		err := config.WriteBaseline(baselineFile, prunedBaseline)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Error saving pruned baseline: %v\n", err)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "✨ Baseline successfully pruned and saved to: %s\n", baselineFile)
		return
	}

	// 7. Handle Baseline Generation
	if cfg.GenerateBaseline {
		err := generateBaselineFile(rootPath, cfg, results)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// 8. Report Results
	reportAllFindings(rep, results, rootPath, foldsOutOfRequest(cfg))

	if !rep.PrintSummary(results, rootPath) {
		os.Exit(1)
	}
}

// runAudit detects the project, collects and audits its files, then ranks the
// findings by reachability. It is the whole CLI pipeline before reporting.
func runAudit(cfg *config.Config, rootPath string, rep reporter.Reporter) ([]symbol.AuditStatus, config.Baseline, error) {
	project, err := loadAuditProject(rootPath, *cfg)
	if err != nil {
		return nil, config.Baseline{}, err
	}

	results, baseline := project.audit(cfg, func(files int) {
		// Silence header for machine-readable formats
		if cfg.OutputFormat != "llm" && cfg.OutputFormat != "json" {
			rep.PrintHeader(files)
		}
	})
	return results, baseline, nil
}

// auditProject is what an audit loads once for a project: the container dump and the Symfony or Laravel
// service map, whose export (bin/console, php artisan igor:dump) is the slow part. A long-running process,
// such as an MCP server, keeps it to audit the project or one of its files many times.
type auditProject struct {
	rootPath  string
	nonShared analyzer.NonSharedServiceMap
	aliases   analyzer.AliasesMap
	bridge    *auditor.SymfonyBridge
	// degraded tells why the Deep Audit of a detected framework fell back to a directory scan, if it did.
	degraded string
}

// loadAuditProject loads the container dump and detects the framework of the project.
func loadAuditProject(rootPath string, cfg config.Config) (*auditProject, error) {
	p := &auditProject{rootPath: rootPath}

	// Generic container dump (non-shared/transient classes to skip)
	p.nonShared, p.aliases = loadContainerDump(rootPath, cfg)

	// Symfony, then Laravel
	bridge, degraded, err := detectFrameworkProject(rootPath, cfg)
	if err != nil {
		return nil, err
	}
	p.bridge, p.degraded = bridge, degraded
	return p, nil
}

// audit runs one audit of the project with cfg (the whole project, or cfg.TargetFile and its unsaved
// content). Each run starts from a new auditor, so nothing found by a run leaks into the next one.
// onCollected, if set, receives the number of files to audit before the audit starts.
func (p *auditProject) audit(cfg *config.Config, onCollected func(files int)) ([]symbol.AuditStatus, config.Baseline) {
	aud := auditor.NewAuditor(*cfg)
	if cfg.StdinContent != nil && cfg.TargetFile != "" {
		aud.SetFileOverride(cfg.TargetFile, cfg.StdinContent)
	}
	aud.NonSharedServices = p.nonShared
	aud.UseFramework(p.bridge)
	if len(p.aliases) > 0 {
		aud.LoadInterfaceAliases(p.aliases)
	}

	baseline := loadAuditBaseline(p.rootPath, cfg)
	auditList := collectFiles(p.rootPath, *cfg, aud)
	if onCollected != nil {
		onCollected(len(auditList))
	}

	results := executeAudit(auditList, aud, *cfg, baseline, p.rootPath)

	// Rank findings by call-site reachability from application code
	aud.MarkReachability(results)

	// Flag the findings of code that never runs during a request; the CLI folds them
	var extends func(class, ancestor string) bool
	if p.bridge != nil && p.bridge.Framework == auditor.FrameworkSymfony {
		extends = aud.Extends
	}
	markOutOfRequest(results, extends)
	if foldsOutOfRequest(*cfg) {
		foldOutOfRequest(results)
	}

	return results, baseline
}
