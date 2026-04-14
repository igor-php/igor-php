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
	versionFlag := flag.Bool("version", false, "Display version information")
	consoleFlag := flag.String("console", "", "Custom path to Symfony console (e.g. app/console)")
	envFlag := flag.String("env", "", "Symfony environment (default: prod)")
	verboseFlag := flag.Bool("verbose", false, "Enable verbose output to see skipped services and details")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🧟 Igor-PHP v%s - The faithful assistant for FrankenPHP Workers\n\n", Version)
		fmt.Fprintf(os.Stderr, "Usage: igor-php [options] <directory>\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  igor-php .\n")
		fmt.Fprintf(os.Stderr, "  igor-php --env stage --verbose ./my-project\n")
		fmt.Fprintf(os.Stderr, "  igor-php --console app/console .\n")
	}

	flag.Parse()

	if *versionFlag {
		fmt.Printf("igor-php version %s\n", Version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: igor-php <directory>")
		os.Exit(1)
	}
	rootPath, _ := filepath.Abs(args[0])

	// 1. Initialize Components
	config := LoadConfig(rootPath)
	if *consoleFlag != "" {
		config.ConsolePath = *consoleFlag
	}
	if *envFlag != "" {
		config.Env = *envFlag
	}
	if *verboseFlag {
		config.Verbose = true
	}
	auditor := NewAuditor(config)
	reporter := NewReporter()

	// 2. Detect Symfony project
	sb, err := DetectSymfony(rootPath, config)
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		os.Exit(1)
	}
	auditor.Symfony = sb

	// 3. Collect Files to Audit
	auditList := collectFiles(rootPath, config, auditor)

	reporter.PrintHeader(len(auditList))

	// 4. Parallel Audit
	results := runParallelAudit(auditList, auditor)

	// 5. Report Results
	var finalResults []AuditStatus
	for res := range results {
		finalResults = append(finalResults, res)
		reporter.PrintFindings(res, rootPath)
	}

	success := reporter.PrintSummary(finalResults)
	if !success {
		os.Exit(1)
	}
}

func collectFiles(rootPath string, config Config, auditor *Auditor) []AuditStatus {
	var auditList []AuditStatus
	if auditor.Symfony != nil {
		fmt.Println("🎯 Deep Audit mode: Auditing ALL shared services (including vendors)...")
		processedFiles := make(map[string]bool)
		for id, def := range auditor.Symfony.Container.Definitions {
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

			if path, found := auditor.Symfony.ClassToFile[def.Class]; found {
				if !processedFiles[path] {
					auditList = append(auditList, AuditStatus{ServiceID: id, FilePath: path, Status: "⏳ PENDING"})
					processedFiles[path] = true
				} else if config.Verbose {
					fmt.Printf("  ⏭️  Skipped service '%s': file already scheduled for audit\n", id)
				}
			} else if config.Verbose {
				fmt.Printf("  ⏭️  Skipped service '%s': could not locate file for class %s\n", id, def.Class)
			}
		}
	} else {
		fmt.Println("ℹ️  Standard mode: Scanning directory for PHP files.")
		_ = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".php") {
				return nil
			}
			rel, _ := filepath.Rel(rootPath, path)
			for _, ex := range config.Exclude {
				if strings.Contains(rel, ex+string(os.PathSeparator)) {
					return nil
				}
			}
			auditList = append(auditList, AuditStatus{ServiceID: "N/A", FilePath: path, Status: "⏳ PENDING"})
			return nil
		})
	}
	return auditList
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
					job.Status = "❌ KO"
					for _, f := range findings {
						if f.Severity == "WARNING" {
							job.Status = "⚠️  WARN"
							break
						}
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
