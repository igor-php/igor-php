package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/internal/fixer"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// fixerFlag implements a flag usable both as --fixer and --fixer=risky.
type fixerFlag struct {
	level string
}

func (f *fixerFlag) String() string {
	if f == nil {
		return ""
	}
	return f.level
}

func (f *fixerFlag) IsBoolFlag() bool { return true }

func (f *fixerFlag) Set(value string) error {
	switch value {
	case "true", "safe":
		f.level = string(fixer.LevelSafe)
	case "risky":
		f.level = string(fixer.LevelRisky)
	case "false":
		f.level = ""
	default:
		return fmt.Errorf("invalid fixer level %q (expected safe or risky)", value)
	}
	return nil
}

// fixSummary counts the outcome of a fixer run.
type fixSummary struct {
	fixes    int
	files    int
	skipped  int
	failures int
}

// passed combines the audit outcome with the fixer's: a failed fix fails the run.
func (s fixSummary) passed(auditPassed bool) bool {
	return auditPassed && s.failures == 0
}

// validateFixerFlags rejects flag combinations the fixer cannot honor.
func validateFixerFlags(level, stdinFilePath string, generateBaseline, checkBaseline, pruneBaseline bool) error {
	if level != "" && (stdinFilePath != "" || generateBaseline || checkBaseline || pruneBaseline) {
		return fmt.Errorf("--fixer cannot be combined with --stdin-filepath or baseline management flags")
	}
	return nil
}

// applyFixesAndReaudit runs the fixer when requested and, if files changed,
// audits again so the report shows what remains.
func applyFixesAndReaudit(results, auditList []symbol.AuditStatus, aud *auditor.Auditor, cfg config.Config, baseline config.Baseline, rootPath string) ([]symbol.AuditStatus, fixSummary) {
	if cfg.FixLevel == "" {
		return results, fixSummary{}
	}
	summary := runFixer(results, rootPath, cfg)
	if summary.files == 0 {
		return results, summary
	}
	results = executeAudit(auditList, aud, cfg, baseline, rootPath)
	aud.MarkReachability(results)
	return results, summary
}

// runFixer applies the fixes to every project file with findings. Vendor files
// are never modified.
func runFixer(results []symbol.AuditStatus, rootPath string, cfg config.Config) fixSummary {
	opts := fixer.Options{
		Level:             fixer.Level(cfg.FixLevel),
		HasResetInterface: hasResetInterface(rootPath),
	}

	var summary fixSummary
	for _, res := range results {
		if len(res.Findings) == 0 || res.IsVendor(rootPath) {
			continue
		}
		path := res.FilePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(rootPath, path)
		}
		fixFile(path, res.Findings, opts, cfg.Verbose, &summary)
	}

	fmt.Fprintf(os.Stderr, "🔧 Fixer: %d fix(es) applied in %d file(s), %d fixable finding(s) left for manual review.\n",
		summary.fixes, summary.files, summary.skipped)
	return summary
}

func fixFile(path string, findings []symbol.Finding, opts fixer.Options, verbose bool, summary *fixSummary) {
	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Fixer: cannot read %s: %v\n", path, err)
		summary.failures++
		return
	}
	result, err := fixer.Apply(content, findings, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Fixer: %s left unchanged: %v\n", path, err)
		summary.failures++
		return
	}

	summary.skipped += len(result.Skipped)
	if verbose {
		for _, skip := range result.Skipped {
			fmt.Fprintf(os.Stderr, "  ⏭️  %s:%d [%s] %s\n", path, skip.Line, skip.Rule, skip.Reason)
		}
	}
	if len(result.Applied) == 0 {
		return
	}
	if err := writeFileAtomic(path, result.Content); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Fixer: cannot write %s: %v\n", path, err)
		summary.failures++
		return
	}
	summary.fixes += len(result.Applied)
	summary.files++
	for _, change := range result.Applied {
		fmt.Fprintf(os.Stderr, "  ✅ %s:%d [%s] %s\n", path, change.Line, change.Rule, change.Description)
	}
}

// writeFileAtomic replaces path through a temporary file, keeping its mode and
// following symlinks so the link itself is preserved.
func writeFileAtomic(path string, content []byte) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".igor-fix-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

func hasResetInterface(rootPath string) bool {
	for _, candidate := range []string{
		filepath.Join("vendor", "symfony", "service-contracts", "ResetInterface.php"),
		filepath.Join("vendor", "symfony", "contracts", "Service", "ResetInterface.php"),
	} {
		if _, err := os.Stat(filepath.Join(rootPath, candidate)); err == nil {
			return true
		}
	}
	return false
}
