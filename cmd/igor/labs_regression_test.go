package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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
			if _, err := os.Stat(filepath.Join(root, "vendor", "autoload.php")); err != nil {
				return "dependencies are not installed (composer install)"
			}
			if _, err := os.Stat(filepath.Join(root, "var", "cache", "dev", "igor_service_map.json")); err != nil {
				return "the Igor service map is missing (php bin/console cache:clear)"
			}
			return requirePHPVersion(80400)
		},
	},
	{
		// No Laravel bridge yet: a plain directory scan, which only needs the sources
		name:          "demo-leak-laravel",
		prerequisites: func(string) string { return "" },
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
			if reason := lab.prerequisites(root); reason != "" {
				if os.Getenv(labsRequiredEnv) != "" {
					t.Fatalf("%s: %s", lab.name, reason)
				}
				t.Skipf("%s: %s (set %s=1 to make this a failure)", lab.name, reason, labsRequiredEnv)
			}

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

// requirePHPVersion returns why the local PHP cannot run the lab, or "" when it can.
func requirePHPVersion(minID int) string {
	out, err := exec.Command("php", "-r", "echo PHP_VERSION_ID;").Output()
	if err != nil {
		return "`php` is not available"
	}
	id, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || id < minID {
		return fmt.Sprintf("PHP %s is too old (needs PHP_VERSION_ID >= %d)", strings.TrimSpace(string(out)), minID)
	}
	return ""
}
