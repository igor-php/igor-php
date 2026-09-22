package main

import (
	"os/exec"
	"testing"
)

// requirePHP skips the test when no `php` binary is on PATH. Igor's PHP-dependent
// paths (Symfony container introspection, reflection-based file location, `php -l`
// syntax checks) can only run where PHP is installed — e.g. inside a container
// with PHP — so on a PHP-less host these tests skip rather than fail.
func requirePHP(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("skipping: `php` not found in PATH (run PHP-dependent tests in a PHP-enabled environment)")
	}
}

func TestPhpWrapperSyntax(t *testing.T) {
	requirePHP(t)
	files := []string{
		"../../internal/auditor/find_class_files.php",
		"../../src/php/IgorPhpBundle.php",
		"../../src/php/DependencyInjection/Compiler/IgorDiscoveryPass.php",
	}
	for _, f := range files {
		cmd := exec.Command("php", "-l", f)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("PHP syntax check failed for %s:\n%s", f, string(output))
		}
	}
}
