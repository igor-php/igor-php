package main

import (
	"os/exec"
	"testing"
)

func TestPhpWrapperSyntax(t *testing.T) {
	cmd := exec.Command("php", "-l", "find_class_files.php")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("PHP syntax check failed for find_class_files.php:\n%s", string(output))
	}
}
