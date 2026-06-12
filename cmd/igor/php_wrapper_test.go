package main

import (
	"os/exec"
	"testing"
)

// requirePHP skips the test when no `php` binary is on PATH. Igor's PHP-dependent
// paths (Symfony container introspection, reflection-based file location, `php -l`
// syntax checks) can only run where PHP is installed — e.g. inside the waffle-dev
// container — so on a PHP-less host these tests skip rather than fail.
func requirePHP(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("skipping: `php` not found in PATH (run PHP-dependent tests inside waffle-dev)")
	}
}

func TestPhpWrapperSyntax(t *testing.T) {
	requirePHP(t)
	cmd := exec.Command("php", "-l", "../../internal/auditor/find_class_files.php")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("PHP syntax check failed for find_class_files.php:\n%s", string(output))
	}
}
