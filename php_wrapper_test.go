package main

import (
	"os/exec"
	"testing"
)

func TestPhpWrapperSyntax(t *testing.T) {
	cmd := exec.Command("php", "-l", "bin/igor-php")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("PHP syntax check failed for bin/igor-php:\n%s", string(output))
	}
}
