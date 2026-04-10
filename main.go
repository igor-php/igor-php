package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type AuditStatus struct {
	ServiceID string
	FilePath  string
	Status    string // "✅ OK", "❌ KO", "⚠️  WARN", "❓ MISSING"
	Findings  []Finding
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: igor-php <directory>")
		os.Exit(1)
	}
	rootPath, _ := filepath.Abs(args[0])
	start := time.Now()

	config := loadConfig(rootPath)
	auditor := NewServiceAuditor(config) // Pass config here

	// 1. Detect Symfony
	isSymfony := false
	projectRoot := rootPath
	if _, err := os.Stat(filepath.Join(projectRoot, "bin", "console")); err != nil {
		projectRoot = filepath.Dir(rootPath)
	}
	
	if _, err := os.Stat(filepath.Join(projectRoot, "bin", "console")); err == nil {
		if err := auditor.LoadSymfonyContainer(projectRoot); err == nil {
			isSymfony = true
		}
	}

	// 2. Prepare Audit List
	var auditList []AuditStatus
	if isSymfony {
		fmt.Println("🎯 Deep Audit mode: Auditing ALL shared services (including vendors)...")
		processedFiles := make(map[string]bool)
		
		for id, def := range auditor.container.Definitions {
			if strings.HasPrefix(id, ".errored.") { continue }

			if def.Shared && def.Class != "" {
				path, found := auditor.classToFile[def.Class]
				if found {
					if !processedFiles[path] {
						auditList = append(auditList, AuditStatus{ServiceID: id, FilePath: path, Status: "⏳ PENDING"})
						processedFiles[path] = true
					}
				}
			}
		}
	} else {
		fmt.Println("⚠️  Non-Symfony project: Standard directory scan mode.")
		_ = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(path, ".php") {
				rel, _ := filepath.Rel(rootPath, path)
				excluded := false
				for _, ex := range config.Exclude {
					if strings.Contains(rel, ex+string(os.PathSeparator)) {
						excluded = true
						break
					}
				}
				if !excluded {
					auditList = append(auditList, AuditStatus{ServiceID: "N/A", FilePath: path, Status: "⏳ PENDING"})
				}
			}
			return nil
		})
	}

	fmt.Printf("🧟 Igor is auditing %d unique shared service files for you, Master...\n\n", len(auditList))

	// 3. Parallel Audit
	var wg sync.WaitGroup
	resultsChan := make(chan AuditStatus, len(auditList))
	jobsChan := make(chan AuditStatus, len(auditList))

	for w := 1; w <= 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobsChan {
				if job.Status == "⏳ PENDING" {
					findings, _ := auditor.Audit(job.FilePath)
					job.Status = "✅ OK"
					job.Findings = findings
					if len(findings) > 0 {
						job.Status = "❌ KO"
						for _, f := range findings {
							if f.Severity == "WARNING" {
								job.Status = "⚠️  WARN"
								break
							}
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
	wg.Wait()
	close(resultsChan)

	// 4. Summarize Results
	totalOK, totalKO, totalWarn := 0, 0, 0
	var failedAudits []AuditStatus

	for res := range resultsChan {
		switch res.Status {
		case "✅ OK": totalOK++
		case "❌ KO": 
			totalKO++
			failedAudits = append(failedAudits, res)
		case "⚠️  WARN": 
			totalWarn++
			failedAudits = append(failedAudits, res)
		}
	}

	// 5. Detailed Errors
	if len(failedAudits) > 0 {
		fmt.Println("\n--- ❌ CONFLICTS DETECTED WITH WORKER MODE ---")
		for _, fa := range failedAudits {
			displayPath := fa.FilePath
			if rel, err := filepath.Rel(projectRoot, fa.FilePath); err == nil {
				displayPath = rel
			}

			fmt.Printf("\n📂 \033[1m%s\033[0m\n", displayPath)
			if fa.ServiceID != "N/A" {
				fmt.Printf("   \033[90mService: %s\033[0m\n", fa.ServiceID)
			}
			for _, f := range fa.Findings {
				color := "\033[31m"
				if f.Severity == "WARNING" { color = "\033[33m" }
				fmt.Printf("  %s%s\033[0m\n", color, f.Message)
				fmt.Printf("  \033[90m%d | %s\033[0m\n", f.Line, strings.TrimSpace(f.Code))
			}
		}
	}

	// 6. Bilan Final
	fmt.Printf("\n--- 🏁 DEEP AUDIT COMPLETE ---")
	fmt.Printf("\nTotal unique service files: %d", totalOK+totalKO+totalWarn)
	fmt.Printf("\n✅ OK (Stateless):           %d", totalOK)
	fmt.Printf("\n❌ KO (Dangerous State):     %d", totalKO)
	fmt.Printf("\n⚠️  WARN (Review reset):      %d", totalWarn)
	fmt.Printf("\nTime taken: %v\n", time.Since(start))

	if totalKO > 0 {
		fmt.Println("\n\033[31m⚠️  DANGER: Your application or its vendors contain services with shared state.")
		fmt.Println("These services will leak data between requests in Worker Mode.\033[0m")
		os.Exit(1)
	} else {
		fmt.Println("\n\033[32m✨ CONGRATULATIONS: Your application and all its dependencies are compatible with Worker Mode!\033[0m")
	}
}
