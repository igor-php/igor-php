package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/igor-php/igor-php/internal/config"
)

func parseFlagsAndInit(args []string) (config.Config, string, bool, error) {
	binName = filepath.Base(args[0])
	if strings.HasPrefix(binName, "main") || strings.HasPrefix(binName, "exe") {
		binName = "igor"
	}

	fs := flag.NewFlagSet(binName, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var configPath string
	versionFlag := fs.Bool("version", false, "Display version information")
	fs.StringVar(&configPath, "config", "", "Custom path to igor.json")
	fs.StringVar(&configPath, "c", "", "Custom path to igor.json (shorthand)")
	baselineFlag := fs.String("baseline", "", "Path to baseline file")
	generateBaselineFlag := fs.Bool("generate-baseline", false, "Generate a baseline file from current findings")
	checkBaselineFlag := fs.Bool("check-baseline", false, "Verify if the baseline is clean (fails if any baseline entries are no longer detected)")
	pruneBaselineFlag := fs.Bool("prune-baseline", false, "Remove stale entries from the baseline automatically")
	consoleFlag := fs.String("console", "", "Custom path to Symfony console (e.g. app/console)")
	envFlag := fs.String("env", "", "Symfony environment (default: dev)")
	verboseFlag := fs.Bool("verbose", false, "Enable verbose output to see skipped services and details")
	noAgentFlag := fs.Bool("no-agent", false, "Disable Igor Agent and fallback to standard scan")
	outputFlag := fs.String("output", "cli", "Output format (cli, llm, json)")
	containerDumpFlag := fs.String("container-dump", "", "Path to a generic container dump JSON ({\"services\":[{\"class\":...,\"shared\":bool}]}) used to skip transient (non-shared) classes")
	ignoreExternalBaselineFlag := fs.Bool("ignore-external-baseline", false, "Ignore baseline files defined in external vendor packages")
	stdinFilePathFlag := fs.String("stdin-filepath", "", "Path of the file when passing file content via standard input (stdin)")
	includeIgnoredFlag := fs.Bool("include-ignored", false, "Include baseline-ignored findings marked with ignored: true")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "🧟 Igor-PHP v%s - The faithful assistant for FrankenPHP Workers\n\n", Version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s [options] <directory>    Audit a project\n", binName)
		fmt.Fprintf(os.Stderr, "  %s init [options] [directory] Initialize a new igor.json config\n", binName)
		fmt.Fprintf(os.Stderr, "  %s review <json_file>       Review an audit JSON export with an LLM\n", binName)
		fmt.Fprintf(os.Stderr, "  %s explain [directory] [filter] Display semantic diagnostics explanation matrix for services\n", binName)
		fmt.Fprintf(os.Stderr, "  %s debug-external-baseline [directory] List all discovered vendor baselines in the project\n\n", binName)
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s .\n", binName)
		fmt.Fprintf(os.Stderr, "  %s --output json .\n", binName)
		fmt.Fprintf(os.Stderr, "  %s --container-dump igor-container.json .\n", binName)
		fmt.Fprintf(os.Stderr, "  %s --generate-baseline\n", binName)
		fmt.Fprintf(os.Stderr, "  %s -c custom-igor.json .\n", binName)
		fmt.Fprintf(os.Stderr, "  %s init\n", binName)
		fmt.Fprintf(os.Stderr, "  %s review igor-export.json\n", binName)
		fmt.Fprintf(os.Stderr, "  %s explain . SuperService\n", binName)
		fmt.Fprintf(os.Stderr, "  %s debug-external-baseline .\n", binName)
		fmt.Fprintf(os.Stderr, "  %s --env stage --verbose ./my-project\n", binName)
	}

	err := fs.Parse(args[1:])
	if err != nil {
		return config.Config{}, "", true, err
	}

	if *versionFlag {
		fmt.Fprintf(os.Stderr, "%s version %s\n", binName, Version)
		return config.Config{}, "", true, nil
	}

	parsedArgs := fs.Args()
	if len(parsedArgs) > 0 {
		switch parsedArgs[0] {
		case "init":
			err := handleInitSubcommand(parsedArgs, configPath)
			return config.Config{}, "", true, err
		case "review":
			err := handleReviewSubcommand(parsedArgs, configPath)
			return config.Config{}, "", true, err
		case "explain":
			err := handleExplainSubcommand(parsedArgs, configPath)
			return config.Config{}, "", true, err
		case "debug-external-baseline":
			err := handleDebugExternalBaselineSubcommand(parsedArgs, configPath)
			return config.Config{}, "", true, err
		}
	}

	var targetFile string
	var rootPath string

	if *stdinFilePathFlag != "" {
		targetPath, _ := filepath.Abs(*stdinFilePathFlag)
		targetFile = targetPath
		rootPath = findProjectRoot(filepath.Dir(targetPath))
	} else {
		if len(parsedArgs) < 1 {
			fs.Usage()
			return config.Config{}, "", true, fmt.Errorf("missing target directory to audit")
		}
		targetPath, _ := filepath.Abs(parsedArgs[0])

		if fi, err := os.Stat(targetPath); err == nil && !fi.IsDir() {
			targetFile = targetPath
			rootPath = findProjectRoot(filepath.Dir(targetPath))
		} else {
			rootPath = targetPath
		}
	}

	if (*stdinFilePathFlag != "" || targetFile != "") && (*generateBaselineFlag || *checkBaselineFlag || *pruneBaselineFlag) {
		return config.Config{}, "", true, fmt.Errorf("baseline management flags (--generate-baseline, --check-baseline, --prune-baseline) cannot be used with a single file target or --stdin-filepath")
	}

	cfg := config.LoadConfig(rootPath, configPath)
	cfg.TargetFile = targetFile

	if *stdinFilePathFlag != "" {
		stdinBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return config.Config{}, "", true, fmt.Errorf("failed to read from standard input: %w", err)
		}
		cfg.StdinContent = stdinBytes
	}
	applyFlagOverrides(&cfg, consoleFlag, envFlag, verboseFlag, noAgentFlag, outputFlag, generateBaselineFlag, baselineFlag, containerDumpFlag, ignoreExternalBaselineFlag, checkBaselineFlag, pruneBaselineFlag, includeIgnoredFlag)

	// Display summary of packages
	if len(cfg.ProdPackages) > 0 || len(cfg.DevPackages) > 0 {
		fmt.Fprintf(os.Stderr, "📦 Composer: %d production packages will be inspected, %d dev packages will be ignored.\n",
			len(cfg.ProdPackages), len(cfg.DevPackages))
		if !*verboseFlag && len(cfg.DevPackages) > 0 {
			fmt.Fprintln(os.Stderr, "   (Use --verbose to see which services are being skipped)")
		}
	}

	return cfg, rootPath, false, nil
}

func applyFlagOverrides(cfg *config.Config, consoleFlag, envFlag *string, verboseFlag, noAgentFlag *bool, outputFlag *string, generateBaselineFlag *bool, baselineFlag, containerDumpFlag *string, ignoreExternalBaselineFlag *bool, checkBaselineFlag, pruneBaselineFlag *bool, includeIgnoredFlag *bool) {
	if *includeIgnoredFlag {
		cfg.IncludeIgnored = true
	}
	if *consoleFlag != "" {
		cfg.ConsolePath = *consoleFlag
	}
	if *containerDumpFlag != "" {
		cfg.ContainerDump = *containerDumpFlag
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
	if *ignoreExternalBaselineFlag {
		cfg.IgnoreExternalBaseline = true
	}
	if *checkBaselineFlag {
		cfg.CheckBaseline = true
	}
	if *pruneBaselineFlag {
		cfg.PruneBaseline = true
	}
	switch {
	case *generateBaselineFlag:
		cfg.GenerateBaseline = true
		if *baselineFlag != "" {
			cfg.BaselinePath = *baselineFlag
		} else if cfg.BaselinePath == "" {
			cfg.BaselinePath = "igor-baseline.json"
		}
	case *baselineFlag != "":
		cfg.BaselinePath = *baselineFlag
	case cfg.BaselinePath == "" && (cfg.CheckBaseline || cfg.PruneBaseline):
		cfg.BaselinePath = "igor-baseline.json"
	}
}

func findProjectRoot(startDir string) string {
	clean := filepath.Clean(startDir)
	slashPath := filepath.ToSlash(clean)
	// If the file is inside a vendor/ directory, locate the host project root containing vendor/
	if idx := strings.LastIndex(slashPath, "/vendor/"); idx != -1 {
		hostCandidate := clean[:idx]
		if _, err := os.Stat(filepath.Join(hostCandidate, "composer.json")); err == nil {
			return hostCandidate
		}
	} else if strings.HasPrefix(slashPath, "vendor/") {
		if _, err := os.Stat("composer.json"); err == nil {
			return "."
		}
	}

	curr := startDir
	for {
		// Do not treat a package inside vendor/ as the project root if it has a vendor ancestor
		isInsideVendor := strings.Contains(filepath.ToSlash(curr), "/vendor/")
		if !isInsideVendor {
			if _, err := os.Stat(filepath.Join(curr, "composer.json")); err == nil {
				return curr
			}
			if _, err := os.Stat(filepath.Join(curr, "bin", "console")); err == nil {
				return curr
			}
			if _, err := os.Stat(filepath.Join(curr, "igor.json")); err == nil {
				return curr
			}
		}
		parent := filepath.Dir(curr)
		if parent == curr || parent == "." || parent == "/" {
			if _, err := os.Stat(filepath.Join(parent, "composer.json")); err == nil {
				return parent
			}
			break
		}
		curr = parent
	}
	return startDir
}

