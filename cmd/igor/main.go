package main

import (
        "bytes"
        "flag"
        "fmt"
        "os"
        "os/exec"
        "path/filepath"
        "strings"
        "sync"
	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/reporter"
	"github.com/igor-php/igor-php/pkg/symbol"
)

var Version = "dev"

func main() {
	cfg, rootPath, shouldExit := parseFlagsAndInit()
	if shouldExit {
		return
	}

	// 1. Initialize Components
	aud := auditor.NewAuditor(cfg)
	var rep reporter.Reporter
	if cfg.OutputFormat == "llm" {
		rep = reporter.NewLLMReporter(Version)
	} else {
		rep = reporter.NewReporter()
	}

	// 2. Detect Symfony project
	sb, err := auditor.DetectSymfony(rootPath, cfg)
	if err != nil {
	        fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
	        os.Exit(1)
	}
	aud.Symfony = sb

	// 3. Load Baseline if exists
	var baseline config.Baseline
	if cfg.BaselinePath != "" && !cfg.GenerateBaseline {
	        baselineFile := cfg.BaselinePath
	        if !filepath.IsAbs(baselineFile) {
	                baselineFile = filepath.Join(rootPath, baselineFile)
	        }
	        baseline, err = config.LoadBaseline(baselineFile)
	        if err != nil {
	                fmt.Fprintf(os.Stderr, "⚠️  Warning: Could not load baseline from %s: %v\n", baselineFile, err)
	        } else {
	                fmt.Fprintf(os.Stderr, "🛡️  Baseline loaded: %d files will be partially ignored.\n", len(baseline.Files))
	        }
	}

	// 4. Collect Files to Audit
	auditList := collectFiles(rootPath, cfg, aud)

	rep.PrintHeader(len(auditList))

	// 5. Parallel Audit
	resultsChan := runParallelAudit(auditList, aud)

	// 6. Collect Results
	var finalResults []symbol.AuditStatus
	for res := range resultsChan {
		if !cfg.GenerateBaseline && baseline.Files != nil {
			res.Findings = config.FilterFindings(baseline, res.FilePath, res.Findings, rootPath)
			// Re-calculate status after filtering
			res.Status = "✅ OK"
			if len(res.Findings) > 0 {
				hasError := false
				for _, f := range res.Findings {
					if f.Severity == "ERROR" {
						hasError = true
						break
					}
				}
				if hasError {
					res.Status = "❌ KO"
				} else {
					res.Status = "⚠️  WARN"
				}
			}
		}
		finalResults = append(finalResults, res)
	}

	// 7. Handle Baseline Generation
	if cfg.GenerateBaseline {
	        baselineFile := cfg.BaselinePath
	        if !filepath.IsAbs(baselineFile) {
	                baselineFile = filepath.Join(rootPath, baselineFile)
	        }
	        err := config.SaveBaseline(baselineFile, finalResults, rootPath)
	        if err != nil {
	                fmt.Fprintf(os.Stderr, "❌ Error saving baseline: %v\n", err)
	                os.Exit(1)
	        }
	        fmt.Fprintf(os.Stderr, "\n✨ Baseline successfully generated at: %s\n", baselineFile)
	        fmt.Fprintln(os.Stderr, "👉 Future audits will ignore these existing findings.")
	        return
	}

	// 8. Report Results (Project first, then Vendor)
	reportAllFindings(rep, finalResults, rootPath)

	success := rep.PrintSummary(finalResults, rootPath)
	if !success {
	        os.Exit(1)
	}
	}

	func parseFlagsAndInit() (config.Config, string, bool) {
	var configPath string
	versionFlag := flag.Bool("version", false, "Display version information")
	flag.StringVar(&configPath, "config", "", "Custom path to igor.json")
	flag.StringVar(&configPath, "c", "", "Custom path to igor.json (shorthand)")
	baselineFlag := flag.String("baseline", "", "Path to baseline file")
	generateBaselineFlag := flag.Bool("generate-baseline", false, "Generate a baseline file from current findings")
	consoleFlag := flag.String("console", "", "Custom path to Symfony console (e.g. app/console)")
	envFlag := flag.String("env", "", "Symfony environment (default: dev)")
	verboseFlag := flag.Bool("verbose", false, "Enable verbose output to see skipped services and details")
	noAgentFlag := flag.Bool("no-agent", false, "Disable Igor Agent and fallback to standard scan")
	outputFlag := flag.String("output", "cli", "Output format (cli, llm)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🧟 Igor-PHP v%s - The faithful assistant for FrankenPHP Workers\n\n", Version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  igor-php [options] <directory>    Audit a project\n")
		fmt.Fprintf(os.Stderr, "  igor-php init [options] [directory] Initialize a new igor.json config\n")
		fmt.Fprintf(os.Stderr, "  igor-php review <json_file>       Review an audit JSON export with an LLM\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  igor-php .\n")
		fmt.Fprintf(os.Stderr, "  igor-php --generate-baseline\n")
		fmt.Fprintf(os.Stderr, "  igor-php -c custom-igor.json .\n")
		fmt.Fprintf(os.Stderr, "  igor-php init\n")
		fmt.Fprintf(os.Stderr, "  igor-php review igor-export.json\n")
		fmt.Fprintf(os.Stderr, "  igor-php --env stage --verbose ./my-project\n")
	}

	flag.Parse()

	if *versionFlag {
		fmt.Fprintf(os.Stderr, "igor-php version %s\n", Version)
		return config.Config{}, "", true
	}

	args := flag.Args()
	if len(args) > 0 {
		switch args[0] {
		case "init":
			handleInitSubcommand(args, configPath)
			return config.Config{}, "", true
		case "review":
			handleReviewSubcommand(args, configPath)
			return config.Config{}, "", true
		}
	}

	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}
	rootPath, _ := filepath.Abs(args[0])

	cfg := config.LoadConfig(rootPath, configPath)
	applyFlagOverrides(&cfg, consoleFlag, envFlag, verboseFlag, noAgentFlag, outputFlag, generateBaselineFlag, baselineFlag)

	// Display summary of packages
	if len(cfg.ProdPackages) > 0 || len(cfg.DevPackages) > 0 {
		fmt.Fprintf(os.Stderr, "📦 Composer: %d production packages will be inspected, %d dev packages will be ignored.\n",
			len(cfg.ProdPackages), len(cfg.DevPackages))
		if !*verboseFlag && len(cfg.DevPackages) > 0 {
			fmt.Fprintln(os.Stderr, "   (Use --verbose to see which services are being skipped)")
		}
	}

	return cfg, rootPath, false
}

func handleInitSubcommand(args []string, configPath string) {
	targetDir := "."
	if len(args) > 1 {
		targetDir = args[1]
	}
	rootPath, _ := filepath.Abs(targetDir)
	detectedType, err := config.InitConfig(rootPath, configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
		os.Exit(1)
	}

	actualConfigPath := configPath
	if actualConfigPath == "" {
		actualConfigPath = filepath.Join(rootPath, "igor.json")
	}

	fmt.Fprintf(os.Stderr, "✨ Igor has successfully initialized your project!\n")
	fmt.Fprintf(os.Stderr, "📂 Detected project type: %s\n", detectedType)
	fmt.Fprintf(os.Stderr, "📝 Configuration saved to: %s\n", actualConfigPath)
	fmt.Fprintf(os.Stderr, "👉 You can now customize the configuration to fit your needs.\n")
}

func handleReviewSubcommand(args []string, configPath string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "❌ Error: missing JSON file to review.")
		fmt.Fprintln(os.Stderr, "Usage: igor-php review <json_file>")
		os.Exit(1)
	}
	jsonFile := args[1]
	content, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: could not read file %s: %v\n", jsonFile, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "🧟 Igor is preparing to review %s...\n", jsonFile)

	rootPath, _ := filepath.Abs(".")
	cfg := config.LoadConfig(rootPath, configPath)

	if cfg.LLMConfig.Provider == "gemini" {
		handleGeminiReview(string(content), cfg)
		os.Exit(0)
	}

	if cfg.LLMConfig.Provider == "ollama" || (cfg.LLMConfig.Provider == "openai" && cfg.LLMConfig.APIUrl != "") {
		handleAPIReview(string(content), cfg)
		os.Exit(0)
	}

	err = reporter.GenerateFrictionlessPrompt(string(content))
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "📄 Prompt ready! Copy the content of igor-review-prompt.md to your favorite LLM.")
	os.Exit(0)
}

func handleGeminiReview(content string, cfg config.Config) {
	fmt.Fprintln(os.Stderr, "🤖 Gemini CLI Mode: Sending audit to Gemini...")
	prompt := fmt.Sprintf(reporter.FrictionlessPromptTemplate, content)

	args := []string{"-p", prompt, "--skip-trust"}
	if cfg.LLMConfig.Model != "" {
		args = append(args, "-m", cfg.LLMConfig.Model)
	}

	cmd := exec.Command("gemini", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Gemini CLI failed: %v\n", err)
		os.Exit(1)
	}

	err = os.WriteFile("igor-review.md", out.Bytes(), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error writing review file: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "✨ Gemini Review complete! Results saved to igor-review.md")
}

func handleAPIReview(content string, cfg config.Config) {
	apiUrl := cfg.LLMConfig.APIUrl
	apiKey := ""
	modeName := "Expert Mode"

	if cfg.LLMConfig.Provider == "ollama" {
		modeName = "Ollama Mode"
		if apiUrl == "" {
			apiUrl = "http://localhost:11434/v1"
		}
		apiKey = "ollama"
	} else {
		apiKey = os.Getenv(cfg.LLMConfig.ApiKeyEnv)
		if apiKey == "" {
			fmt.Fprintf(os.Stderr, "⚠️  Expert Mode enabled but ENV %s is empty. Falling back to Frictionless Mode.\n", cfg.LLMConfig.ApiKeyEnv)
			return
		}
	}

	fmt.Fprintf(os.Stderr, "🧠 %s: Sending audit to LLM (%s)...\n", modeName, cfg.LLMConfig.Model)
	client := reporter.NewLLMClient(apiUrl, apiKey, cfg.LLMConfig.Model)

	prompt := fmt.Sprintf(reporter.FrictionlessPromptTemplate, content)
	review, err := client.Review(prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s failed: %v\n", modeName, err)
		os.Exit(1)
	}

	err = os.WriteFile("igor-review.md", []byte(review), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error writing review file: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "✨ %s Review complete! Results saved to igor-review.md\n", modeName)
	os.Exit(0)
}

func applyFlagOverrides(cfg *config.Config, consoleFlag, envFlag *string, verboseFlag, noAgentFlag *bool, outputFlag *string, generateBaselineFlag *bool, baselineFlag *string) {
	if *consoleFlag != "" {
		cfg.ConsolePath = *consoleFlag
	}
	if *envFlag != "" {
		cfg.Env = *envFlag
	}
	if *verboseFlag {
		cfg.Verbose = true
	}
	if *noAgentFlag {
		cfg.NoAgent = true
	}
	if *outputFlag != "" {
		cfg.OutputFormat = *outputFlag
	}
	if *generateBaselineFlag {
		cfg.GenerateBaseline = true
		if *baselineFlag != "" {
			cfg.BaselinePath = *baselineFlag
		} else if cfg.BaselinePath == "" {
			cfg.BaselinePath = "igor-baseline.json"
		}
	} else if *baselineFlag != "" {
		cfg.BaselinePath = *baselineFlag
	}
}
func reportAllFindings(rep reporter.Reporter, results []symbol.AuditStatus, rootPath string) {
	// Report Project Results first
	hasProjectFindings := false
	for _, res := range results {
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
	for _, res := range results {
		isVendor := res.IsVendor(rootPath)
		if isVendor && len(res.Findings) > 0 {
			if !hasVendorFindings {
				rep.PrintVendorHeader()
				hasVendorFindings = true
			}
			rep.PrintFindings(res, rootPath, isVendor)
		}
	}
}

func collectFiles(rootPath string, cfg config.Config, aud *auditor.Auditor) []symbol.AuditStatus {
	var auditList []symbol.AuditStatus
	processedFiles := make(map[string]bool)

	// --- STEP 1: Add shared services from Symfony (to get IDs and Dependencies) ---
	if aud.Symfony != nil {
	        fmt.Fprintln(os.Stderr, "🎯 Symfony detected: Mapping services and dependencies...")
	        auditList = append(auditList, collectSymfonyServices(rootPath, cfg, aud, processedFiles)...)
	}

	// --- STEP 2: Scan remaining local project files ---
	auditList = append(auditList, collectLocalFiles(rootPath, cfg, aud, processedFiles)...)

	// --- STEP 3: Forced Vendor Scan ---
	if len(cfg.ScanVendors) > 0 {
	        fmt.Fprintln(os.Stderr, "🔍 Forced Vendor Scan: Auditing specific vendor paths...")
	        auditList = append(auditList, collectForcedVendorFiles(rootPath, cfg, processedFiles)...)
	}
	return auditList
}

func collectLocalFiles(rootPath string, cfg config.Config, aud *auditor.Auditor, processed map[string]bool) []symbol.AuditStatus {
	var list []symbol.AuditStatus
	_ = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".php") {
			return nil
		}
		if cfg.IsExcluded(path, rootPath) {
			return nil
		}
		rel, _ := filepath.Rel(rootPath, path)
		if strings.HasPrefix(rel, "vendor"+string(os.PathSeparator)) || strings.HasPrefix(rel, "var"+string(os.PathSeparator)) {
			return nil
		}
		if aud.IsDataPath(path) {
			return nil
		}
		list = append(list, symbol.AuditStatus{ServiceID: "N/A", FilePath: path, Status: "⏳ PENDING"})
		processed[path] = true
		return nil
	})
	return list
}

func collectSymfonyServices(rootPath string, cfg config.Config, aud *auditor.Auditor, processed map[string]bool) []symbol.AuditStatus {
	var list []symbol.AuditStatus
	for id, def := range aud.Symfony.Container.Definitions {
	        if strings.HasPrefix(id, ".errored.") {
	                if cfg.Verbose {
	                        fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': container error\n", id)
	                }
	                continue
	        }
	        if !def.Shared {
	                if cfg.Verbose {
	                        fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': non-shared (prototype)\n", id)
	                }
	                continue
	        }
	        if def.Class == "" {
	                if cfg.Verbose {
	                        fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': no class defined\n", id)
	                }
	                continue
	        }
	        if aud.IsSafeNamespace(def.Class) {
	                if cfg.Verbose {
	                        fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': class %s belongs to a safe namespace\n", id, def.Class)
	                }
	                continue
	        }

	        if path, found := aud.Symfony.ClassToFile[def.Class]; found {
	                if cfg.IsExcluded(path, rootPath) {
	                        if cfg.Verbose {
	                                fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': path %s is excluded\n", id, path)
	                        }
	                        continue
	                }
	                if aud.IsDevPackagePath(path) {
	                        if cfg.Verbose {
	                                fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': belongs to a dev package\n", id)
	                        }
	                        continue
	                }
	                if !processed[path] {
	                        deps := extractDependencies(def)
	                        list = append(list, symbol.AuditStatus{
	                                ServiceID:    id,
	                                FilePath:     path,
	                                Status:       "⏳ PENDING",
	                                Dependencies: deps,
	                                IsShared:     def.Shared,
	                                IsPublic:     def.Public,
	                        })
	                        processed[path] = true
	                } else if cfg.Verbose {
	                        fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': file already scheduled for audit\n", id)
	                }
	        } else if cfg.Verbose {
	                fmt.Fprintf(os.Stderr, "  ⏭️  Skipped service '%s': could not locate file for class %s\n", id, def.Class)
	        }
	}

	return list
}

func extractDependencies(def symbol.SymfonyService) []string {
	deps := []string{}
	for _, arg := range def.Arguments {
		if m, ok := arg.(map[string]any); ok {
			// In Symfony JSON, service arguments look like {"type": "service", "id": "..."}
			if typeVal, ok := m["type"].(string); ok && typeVal == "service" {
				if idVal, ok := m["id"].(string); ok {
					deps = append(deps, idVal)
				}
			}
		} else if s, ok := arg.(string); ok && strings.HasPrefix(s, "@") {
			// Fallback for simple string references (e.g. @logger)
			deps = append(deps, strings.TrimPrefix(s, "@"))
		}
	}
	return deps
}

func collectForcedVendorFiles(rootPath string, cfg config.Config, processed map[string]bool) []symbol.AuditStatus {
	var list []symbol.AuditStatus
	for _, vendorSubPath := range cfg.ScanVendors {
		fullVendorPath := filepath.Join(rootPath, "vendor", vendorSubPath)
		_ = filepath.Walk(fullVendorPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".php") {
				return nil
			}
			if !processed[path] {
				list = append(list, symbol.AuditStatus{ServiceID: "N/A", FilePath: path, Status: "⏳ PENDING"})
				processed[path] = true
			}
			return nil
		})
	}
	return list
}

func runParallelAudit(auditList []symbol.AuditStatus, aud *auditor.Auditor) <-chan symbol.AuditStatus {
	resultsChan := make(chan symbol.AuditStatus, len(auditList))
	jobsChan := make(chan symbol.AuditStatus, len(auditList))
	var wg sync.WaitGroup

	for w := 1; w <= 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobsChan {
				findings, err := aud.Audit(job.FilePath, job.Dependencies)
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
