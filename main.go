package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const Version = "0.1.0"

func main() {
	versionFlag := flag.Bool("version", false, "Display version information")
	consoleFlag := flag.String("console", "", "Custom path to Symfony console (e.g. app/console)")
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
	auditor := NewAuditor(config)
	reporter := NewReporter()

	// 2. Detect Symfony project
	detectSymfony(rootPath, config, auditor)

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

func detectSymfony(rootPath string, config Config, auditor *Auditor) {
	projectRoot := rootPath
	consolePath := config.ConsolePath

	if _, err := os.Stat(filepath.Join(projectRoot, consolePath)); err != nil {
		projectRoot = filepath.Dir(rootPath)
	}

	if _, err := os.Stat(filepath.Join(projectRoot, consolePath)); err == nil {
		fmt.Printf("🔍 Symfony project detected at %s. Initializing Deep Audit...\n", projectRoot)
		sb := NewSymfonyBridge(projectRoot, consolePath)
		if err := sb.LoadContainer(); err == nil {
			auditor.Symfony = sb
		} else {
			fmt.Printf("⚠️  Warning: Could not load Symfony container: %v\n", err)
			fmt.Println("👉 Falling back to standard directory scan mode.")
		}
	}
}

func collectFiles(rootPath string, config Config, auditor *Auditor) []AuditStatus {
	var auditList []AuditStatus
	if auditor.Symfony != nil {
		fmt.Println("🎯 Deep Audit mode: Auditing ALL shared services (including vendors)...")
		processedFiles := make(map[string]bool)
		for id, def := range auditor.Symfony.Container.Definitions {
			if strings.HasPrefix(id, ".errored.") {
				continue
			}
			if def.Shared && def.Class != "" {
				if path, found := auditor.Symfony.ClassToFile[def.Class]; found {
					if !processedFiles[path] {
						auditList = append(auditList, AuditStatus{ServiceID: id, FilePath: path, Status: "⏳ PENDING"})
						processedFiles[path] = true
					}
				}
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
