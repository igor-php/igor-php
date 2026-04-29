package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var Version = "dev"

func main() {
	config, rootPath, shouldExit := parseFlagsAndInit()
	if shouldExit {
		return
	}

	// 1. Initialize Components
	auditor := NewAuditor(config)
	reporter := NewReporter()

	// 2. Detect Framework (Strategy)
	bridge, err := DetectFramework(rootPath, config)
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		os.Exit(1)
	}
	if bridge != nil {
		auditor.Framework = bridge
	}

	// 3. Collect Files to Audit
	auditList := collectFiles(rootPath, config, auditor)

	reporter.PrintHeader(len(auditList))

	// 4. Parallel Audit
	resultsChan := runParallelAudit(auditList, auditor)

	// 5. Collect Results
	var finalResults []AuditStatus
	for res := range resultsChan {
		finalResults = append(finalResults, res)
	}

	// 6. Report Results (Project first, then Vendor)
	reportAllFindings(reporter, finalResults, rootPath)

	success := reporter.PrintSummary(finalResults, rootPath)
	if !success {
		os.Exit(1)
	}
}

func parseFlagsAndInit() (Config, string, bool) {
	versionFlag := flag.Bool("version", false, "Display version information")
	consoleFlag := flag.String("console", "", "Custom path to console (e.g. bin/console or artisan)")
	pathsFlag := flag.String("paths", "", "Comma-separated list of directories to scan")
	envFlag := flag.String("env", "", "Environment (default: dev/prod)")
	verboseFlag := flag.Bool("verbose", false, "Enable verbose output to see skipped services and details")
	noAgentFlag := flag.Bool("no-agent", false, "Disable Igor Agent and fallback to standard scan")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🧟 Igor-PHP v%s - The faithful assistant for FrankenPHP Workers\n\n", Version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  igor-php [options] <directory>    Audit a project\n")
		fmt.Fprintf(os.Stderr, "  igor-php init [directory]         Initialize a new igor.json config\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  igor-php .\n")
		fmt.Fprintf(os.Stderr, "  igor-php init\n")
		fmt.Fprintf(os.Stderr, "  igor-php --env stage --verbose ./my-project\n")
	}

	flag.Parse()

	if *versionFlag {
		fmt.Printf("igor-php version %s\n", Version)
		return Config{}, "", true
	}

	args := flag.Args()
	if len(args) > 0 && args[0] == "init" {
		targetDir := "."
		if len(args) > 1 {
			targetDir = args[1]
		}
		rootPath, _ := filepath.Abs(targetDir)
		if err := InitConfig(rootPath); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			os.Exit(1)
		}
		return Config{}, "", true
	}

	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}
	rootPath, _ := filepath.Abs(args[0])

	config := LoadConfig(rootPath)
	if *consoleFlag != "" {
		config.ConsolePath = *consoleFlag
	}
	if *pathsFlag != "" {
		config.Paths = strings.Split(*pathsFlag, ",")
	}
	if *envFlag != "" {
		config.Env = *envFlag
	}
	if *verboseFlag {
		config.Verbose = true
	}
	if *noAgentFlag {
		config.NoAgent = true
	}

	// Display summary of packages
	if len(config.ProdPackages) > 0 || len(config.DevPackages) > 0 {
		fmt.Printf("📦 Composer: %d production packages will be inspected, %d dev packages will be ignored.\n",
			len(config.ProdPackages), len(config.DevPackages))
		if !*verboseFlag && len(config.DevPackages) > 0 {
			fmt.Println("   (Use --verbose to see which services are being skipped)")
		}
	}

	return config, rootPath, false
}

func reportAllFindings(reporter *Reporter, results []AuditStatus, rootPath string) {
	// Report Project Results first
	hasProjectFindings := false
	for _, res := range results {
		isVendor := res.IsVendor(rootPath)
		if !isVendor && len(res.Findings) > 0 {
			if !hasProjectFindings {
				fmt.Println("\n\033[34m--- 📂 PROJECT SERVICES ---\033[0m")
				hasProjectFindings = true
			}
			reporter.PrintFindings(res, rootPath, isVendor)
		}
	}

	// Report Vendor Results second
	hasVendorFindings := false
	for _, res := range results {
		isVendor := res.IsVendor(rootPath)
		if isVendor && len(res.Findings) > 0 {
			if !hasVendorFindings {
				fmt.Println("\n\033[33m--- 📦 VENDOR SERVICES (THIRD-PARTY) ---\033[0m")
				hasVendorFindings = true
			}
			reporter.PrintFindings(res, rootPath, isVendor)
		}
	}
}

func collectFiles(rootPath string, config Config, auditor *Auditor) []AuditStatus {
	var auditList []AuditStatus
	processedFiles := make(map[string]bool)

	// --- STEP 1: Determine scan paths ---
	pathsToScan := config.Paths
	if len(pathsToScan) == 0 {
		if auditor.Framework != nil {
			if auditor.Framework.GetName() == "Laravel" {
				pathsToScan = []string{"app"}
			} else if auditor.Framework.GetName() == "Symfony" {
				pathsToScan = []string{"src"}
			}
		}
		if len(pathsToScan) == 0 {
			pathsToScan = []string{"."}
		}
	}

	// --- STEP 2: Scan local project files ---
	for _, p := range pathsToScan {
		fullPath := filepath.Join(rootPath, p)
		auditList = append(auditList, collectLocalFiles(fullPath, rootPath, config, auditor, processedFiles)...)
	}

	// --- STEP 3: Add shared services from vendors (via Framework) ---
	if auditor.Framework != nil {
		fmt.Printf("🎯 %s detected: Auditing shared services from vendors...\n", auditor.Framework.GetName())
		auditList = append(auditList, collectFrameworkServices(config, auditor, processedFiles)...)
	}

	// --- STEP 3: Forced Vendor Scan ---
	if len(config.ScanVendors) > 0 {
		fmt.Println("🔍 Forced Vendor Scan: Auditing specific vendor paths...")
		auditList = append(auditList, collectForcedVendorFiles(rootPath, config, processedFiles)...)
	}

	return auditList
}

func collectLocalFiles(scanPath string, rootPath string, config Config, auditor *Auditor, processed map[string]bool) []AuditStatus {
	var list []AuditStatus
	_ = filepath.Walk(scanPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".php") {
			return nil
		}
		rel, _ := filepath.Rel(rootPath, path)
		for _, ex := range config.Exclude {
			if rel == ex || strings.HasPrefix(rel, ex+string(os.PathSeparator)) {
				return nil
			}
		}
		if strings.HasPrefix(rel, "vendor"+string(os.PathSeparator)) || strings.HasPrefix(rel, "var"+string(os.PathSeparator)) {
			return nil
		}
		if auditor.IsDataPath(path) {
			return nil
		}
		list = append(list, AuditStatus{ServiceID: "N/A", FilePath: path, Status: "⏳ PENDING"})
		processed[path] = true
		return nil
	})
	return list
}

func collectFrameworkServices(config Config, auditor *Auditor, processed map[string]bool) []AuditStatus {
	var list []AuditStatus
	definitions := auditor.Framework.GetDefinitions()
	classToFile := auditor.Framework.GetClassToFileMap()

	for id, def := range definitions {
		if strings.HasPrefix(id, ".errored.") {
			if config.Verbose {
				fmt.Printf("  ⏭️  Skipped service '%s': container error\n", id)
			}
			continue
		}
		if !def.Shared {
			if config.Verbose {
				fmt.Printf("  ⏭️  Skipped service '%s': non-shared (prototype)\n", id)
			}
			continue
		}
		if def.Class == "" {
			if config.Verbose {
				fmt.Printf("  ⏭️  Skipped service '%s': no class defined\n", id)
			}
			continue
		}
		if auditor.isSafeNamespace(def.Class) {
			if config.Verbose {
				fmt.Printf("  ⏭️  Skipped service '%s': class %s belongs to a safe namespace\n", id, def.Class)
			}
			continue
		}

		if path, found := classToFile[def.Class]; found {
			if auditor.IsDevPackagePath(path) {
				if config.Verbose {
					fmt.Printf("  ⏭️  Skipped service '%s': belongs to a dev package\n", id)
				}
				continue
			}
			if !processed[path] {

				list = append(list, AuditStatus{ServiceID: id, FilePath: path, Status: "⏳ PENDING"})
				processed[path] = true
			} else if config.Verbose {
				fmt.Printf("  ⏭️  Skipped service '%s': file already scheduled for audit\n", id)
			}
		} else if config.Verbose {
			fmt.Printf("  ⏭️  Skipped service '%s': could not locate file for class %s\n", id, def.Class)
		}
	}
	return list
}

func collectForcedVendorFiles(rootPath string, config Config, processed map[string]bool) []AuditStatus {
	var list []AuditStatus
	for _, vendorSubPath := range config.ScanVendors {
		fullVendorPath := filepath.Join(rootPath, "vendor", vendorSubPath)
		_ = filepath.Walk(fullVendorPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".php") {
				return nil
			}
			if !processed[path] {
				list = append(list, AuditStatus{ServiceID: "N/A", FilePath: path, Status: "⏳ PENDING"})
				processed[path] = true
			}
			return nil
		})
	}
	return list
}

func runParallelAudit(auditList []AuditStatus, auditor *Auditor) <-chan AuditStatus {
	resultsChan := make(chan AuditStatus, len(auditList))
	jobsChan := make(chan AuditStatus, len(auditList))
	var wg sync.WaitGroup

	for w := 1; w <= 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobsChan {
				findings, err := auditor.Audit(job.FilePath)
				if err != nil {
					job.Status = "❌ ERROR"
					resultsChan <- job
					continue
				}
				job.Findings = findings
				job.Status = "✅ OK"
				if len(findings) > 0 {
					hasError := false
					for _, f := range findings {
						if f.Severity == "ERROR" {
							hasError = true
							break
						}
					}
					if hasError {
						job.Status = "❌ KO"
					} else {
						job.Status = "⚠️  WARN"
					}
				}
				resultsChan <- job
			}
		}()
	}

	for _, job := range auditList {
		jobsChan <- job
	}
	close(jobsChan)

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	return resultsChan
}

// DetectFramework attempts to identify the project framework and returns the appropriate bridge.
func DetectFramework(rootPath string, config Config) (FrameworkBridge, error) {
	// Try Laravel first (via artisan)
	lb, err := DetectLaravel(rootPath, config)
	if err != nil {
		return nil, err
	}
	if lb != nil {
		return lb, nil
	}

	// Fallback to Symfony (via bin/console)
	sb, err := DetectSymfony(rootPath, config)
	if err != nil {
		return nil, err
	}
	if sb != nil {
		return sb, nil
	}

	return nil, nil
}
