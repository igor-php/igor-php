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

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: igor <directory>")
		os.Exit(1)
	}
	rootPath, _ := filepath.Abs(args[0])
	start := time.Now()

	config := loadConfig(rootPath)

	var files []string
	_ = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			name := info.Name()
			for _, ex := range config.Exclude {
				if name == ex {
					return filepath.SkipDir
				}
			}
		}
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".php") {
			files = append(files, path)
		}
		return nil
	})

	fmt.Printf("🧟 Igor is inspecting %d files for you, Master...\n", len(files))

	jobs := make(chan string, len(files))
	results := make(chan Result, len(files))
	var wg sync.WaitGroup
	for w := 1; w <= 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				f, _ := analyzeFile(path)
				results <- Result{FilePath: path, Findings: f}
			}
		}()
	}
	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	go func() {
		wg.Wait()
		close(results)
	}()

	totalV, totalW := 0, 0
	for res := range results {
		if len(res.Findings) > 0 {
			fmt.Printf("\n📂 \033[1m%s\033[0m\n", res.FilePath)
			for _, f := range res.Findings {
				color := "\033[31m"
				if f.Severity == "WARNING" {
					color = "\033[33m"
					totalW++
				} else {
					totalV++
				}

				fmt.Printf("  %s%s\033[0m\n", color, f.Message)
				fmt.Printf("  \033[90m%d | %s\033[0m\n", f.Line, strings.TrimSpace(f.Code))
				if f.Remediation != "" {
					fmt.Printf("  \033[36m💡 Hint: %s\033[0m\n", f.Remediation)
				}
				fmt.Println()
			}
		}
	}

	fmt.Printf("\n--- 🏁 Igor has finished his report in %v ---\n", time.Since(start))
	if totalV == 0 && totalW == 0 {
		fmt.Println("✨ \033[32mMaster, your workers are ready for the lightning!\033[0m")
	} else {
		fmt.Printf("\033[31m❌ Errors: %d\033[0m | \033[33m⚠️  Warnings: %d\033[0m\n", totalV, totalW)
		fmt.Println("\033[33mIgor suggests fixing these before activating the Worker Mode.\033[0m")
	}

	if totalV > 0 {
		os.Exit(1)
	}
}
