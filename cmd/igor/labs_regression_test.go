package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/pkg/symbol"
)

// updateLabsGolden rewrites the lab golden files from the current findings:
//
//	go test ./cmd/igor -run TestLabsRegression -update-labs
var updateLabsGolden = flag.Bool("update-labs", false, "rewrite testdata/labs/*.golden from the current findings")

// labsRequiredEnv makes missing lab prerequisites a failure instead of a skip,
// so the CI job that installs the labs can never silently skip them.
const labsRequiredEnv = "IGOR_LABS_REQUIRED"

type leakLab struct {
	name string
	// prerequisites returns why the lab cannot be audited here, or "" when it can.
	prerequisites func(root string) string
}

var leakLabs = []leakLab{
	{
		// Deep Audit: needs the IgorPhpBundle service map, built by `php bin/console cache:clear`
		name: "demo-leak",
		prerequisites: func(root string) string {
			if reason := requireLabDependencies(root); reason != "" {
				return reason
			}
			if _, err := os.Stat(filepath.Join(root, "var", "cache", "dev", "igor_service_map.json")); err != nil {
				return "the Igor service map is missing (php bin/console cache:clear)"
			}
			return ""
		},
	},
	{
		// Deep Audit: igor runs `php artisan igor:dump`, which boots the Laravel application
		name:          "demo-leak-laravel",
		prerequisites: requireLabDependencies,
	},
}

// TestLabsRegression audits every example lab and compares the findings with
// the reviewed snapshot in testdata/labs/<lab>.golden, so a change in the
// engine can neither drop a detected leak nor add noise without being noticed.
func TestLabsRegression(t *testing.T) {
	for _, lab := range leakLabs {
		t.Run(lab.name, func(t *testing.T) {
			root, err := filepath.Abs(filepath.Join("..", "..", "examples", lab.name))
			if err != nil {
				t.Fatal(err)
			}
			requireLab(t, lab, root)

			got := auditLab(t, root)
			goldenPath := filepath.Join("testdata", "labs", lab.name+".golden")

			if *updateLabsGolden {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("missing golden file %s: run `go test ./cmd/igor -run TestLabsRegression -update-labs`", goldenPath)
			}
			// git may check the golden file out with CRLF line endings on Windows
			if diff := diffLines(strings.ReplaceAll(string(want), "\r\n", "\n"), got); diff != "" {
				t.Errorf("findings on examples/%s changed (- expected, + got):\n%s\n"+
					"If this change is intended, review it and run `go test ./cmd/igor -run TestLabsRegression -update-labs`.",
					lab.name, diff)
			}
		})
	}
}

// auditLab runs the CLI pipeline on a lab and renders its findings as stable,
// sorted lines. Line numbers are left out so editing a lab file only changes
// the snapshot when a finding really appears or disappears.
func auditLab(t *testing.T, root string) string {
	t.Helper()

	cfg, rootPath, _, err := parseFlagsAndInit([]string{"igor", "--output", "json", root})
	if err != nil {
		t.Fatalf("failed to initialize igor: %v", err)
	}
	results, _, err := runAudit(&cfg, rootPath, setupReporter(cfg))
	if err != nil {
		t.Fatalf("audit failed: %v", err)
	}

	var lines []string
	for _, res := range results {
		rel, err := filepath.Rel(rootPath, res.FilePath)
		if err != nil {
			rel = res.FilePath
		}
		for _, f := range res.Findings {
			if f.Ignored {
				continue
			}
			lines = append(lines, formatLabFinding(filepath.ToSlash(rel), f))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func formatLabFinding(file string, f symbol.Finding) string {
	context := f.ContextClass
	if f.ContextMethod != "" {
		context += "::" + f.ContextMethod
	}
	return fmt.Sprintf("%s | %s | %s | %s", file, context, f.Severity, f.Message)
}

// diffLines reports the lines missing from got and the unexpected ones, keeping
// duplicates (the same finding may legitimately appear several times).
func diffLines(want, got string) string {
	count := func(s string) map[string]int {
		m := make(map[string]int)
		for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
			if l != "" {
				m[l]++
			}
		}
		return m
	}
	w, g := count(want), count(got)

	var out []string
	for l, n := range w {
		for i := g[l]; i < n; i++ {
			out = append(out, "- "+l)
		}
	}
	for l, n := range g {
		for i := w[l]; i < n; i++ {
			out = append(out, "+ "+l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][2:] < out[j][2:] })
	return strings.Join(out, "\n")
}

// requireLab skips the test when the lab cannot be audited here, or fails it when
// IGOR_LABS_REQUIRED is set (as in CI, where a skipped lab would hide a regression).
func requireLab(t *testing.T, lab leakLab, root string) {
	t.Helper()
	if reason := lab.prerequisites(root); reason != "" {
		if os.Getenv(labsRequiredEnv) != "" {
			t.Fatalf("%s: %s", lab.name, reason)
		}
		t.Skipf("%s: %s (set %s=1 to make this a failure)", lab.name, reason, labsRequiredEnv)
	}
}

// requireLabDependencies returns why a lab's Composer dependencies cannot run here, or "" when they
// can. It runs the platform check Composer generated for them, so a lab installed for another PHP
// version (or another framework version) is skipped instead of crashing.
func requireLabDependencies(root string) string {
	if _, err := os.Stat(filepath.Join(root, "vendor", "autoload.php")); err != nil {
		return "dependencies are not installed (composer install)"
	}
	check := filepath.Join(root, "vendor", "composer", "platform_check.php")
	if _, err := os.Stat(check); err != nil {
		return "" // the platform check is disabled: trust the installed dependencies
	}
	if out, err := exec.Command("php", check).CombinedOutput(); err != nil {
		// Keep only Composer's explanation: "Your Composer dependencies require a PHP version ..."
		msg := string(out)
		if i := strings.Index(msg, "platform: "); i >= 0 {
			msg = msg[i+len("platform: "):]
		}
		if i := strings.Index(msg, " in /"); i >= 0 {
			msg = msg[:i]
		}
		return "the installed dependencies do not support this PHP: " + strings.TrimSpace(msg)
	}
	return ""
}
