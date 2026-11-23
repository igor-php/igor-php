package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const vendorLeak = "<?php\nclass VendorLeak implements ResetInterface {\n    private int $n = 0;\n    public function run(): void { $this->n++; }\n    public function reset(): void {}\n}\n"

// setupFixerProject copies the fixer fixture into a project with a stub
// ResetInterface and a vendor file that must never be modified.
func setupFixerProject(t *testing.T) (root, source, vendor string) {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "fixer_candidates.php"))
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	source = filepath.Join(root, "src", "Candidates.php")
	vendor = filepath.Join(root, "vendor", "acme", "lib", "VendorLeak.php")
	writeTestFile(t, source, string(fixture))
	writeTestFile(t, vendor, vendorLeak)
	writeTestFile(t, filepath.Join(root, "vendor", "symfony", "service-contracts", "ResetInterface.php"),
		"<?php\nnamespace Symfony\\Contracts\\Service;\ninterface ResetInterface { public function reset(); }\n")
	return root, source, vendor
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestFixerSafeOnlyAppliesResetFix(t *testing.T) {
	root, source, vendor := setupFixerProject(t)

	output, err := exec.Command("go", "run", ".", "--no-agent", "--fixer", root).CombinedOutput()
	if err == nil {
		t.Fatalf("expected a failing exit code for the remaining findings:\n%s", output)
	}

	fixed := readFile(t, source)
	if !strings.Contains(fixed, "$this->count = 0;") {
		t.Errorf("safe fix missing:\n%s", fixed)
	}
	for _, untouched := range []string{"class StatefulService\n", "$_GET['id']", "gc_disable();", "exit(1);"} {
		if !strings.Contains(fixed, untouched) {
			t.Errorf("safe level modified %q:\n%s", untouched, fixed)
		}
	}
	if readFile(t, vendor) != vendorLeak {
		t.Error("vendor file was modified")
	}
}

func TestFixerRiskyAppliesOptInFixes(t *testing.T) {
	root, source, vendor := setupFixerProject(t)

	output, err := exec.Command("go", "run", ".", "--no-agent", "--fixer=risky", root).CombinedOutput()
	if err == nil {
		t.Fatalf("expected a failing exit code for exit():\n%s", output)
	}

	fixed := readFile(t, source)
	for _, want := range []string{
		"$this->count = 0;",
		"class StatefulService implements ResetInterface\n",
		"$this->items = [];",
		"$request->query->all()['id'] ?? ''",
		"exit(1);",
	} {
		if !strings.Contains(fixed, want) {
			t.Errorf("missing %q:\n%s", want, fixed)
		}
	}
	if strings.Contains(fixed, "gc_disable();") {
		t.Errorf("gc_disable was not removed:\n%s", fixed)
	}
	if !strings.Contains(fixed, "{\n        return $request") {
		t.Errorf("removal left a stray blank line:\n%s", fixed)
	}
	if readFile(t, vendor) != vendorLeak {
		t.Error("vendor file was modified")
	}
	// The final report reflects the re-audit: only the unfixable finding remains in src.
	report := string(output)
	if !strings.Contains(report, "exit/die") || strings.Contains(report, "Mutation of state 'items'") {
		t.Errorf("report does not reflect the fixed code:\n%s", report)
	}
}

func TestFixerRespectsBaseline(t *testing.T) {
	root, source, _ := setupFixerProject(t)
	baseline := filepath.Join(root, "igor-baseline.json")
	if out, err := exec.Command("go", "run", ".", "--no-agent", "--generate-baseline", "--baseline", baseline, root).CombinedOutput(); err != nil {
		t.Fatalf("baseline generation failed: %v\n%s", err, out)
	}
	before := readFile(t, source)

	if out, err := exec.Command("go", "run", ".", "--no-agent", "--baseline", baseline, "--fixer=risky", root).CombinedOutput(); err != nil {
		t.Fatalf("expected a clean run with a full baseline: %v\n%s", err, out)
	}
	if readFile(t, source) != before {
		t.Error("baselined findings were fixed")
	}
}

func TestFixerRejectsIncompatibleFlags(t *testing.T) {
	root, _, _ := setupFixerProject(t)
	for _, args := range [][]string{
		{"--fixer", "--generate-baseline", root},
		{"--fixer", "--stdin-filepath", filepath.Join(root, "src", "Candidates.php")},
		{"--fixer=unknown", root},
	} {
		cmd := exec.Command("go", append([]string{"run", ".", "--no-agent"}, args...)...)
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Errorf("expected %v to fail:\n%s", args, out)
		}
	}
}

func TestWriteFileAtomicKeepsModeAndSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.php")
	link := filepath.Join(dir, "link.php")
	if err := os.WriteFile(target, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := writeFileAtomic(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if readFile(t, target) != "new" {
		t.Error("target was not updated")
	}
	if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced")
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0640 {
		t.Errorf("mode changed to %v", info.Mode().Perm())
	}
}
