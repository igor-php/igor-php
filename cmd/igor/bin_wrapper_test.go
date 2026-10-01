package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setupWrapperSandbox copies bin/igor-php into an isolated tree with a cached "latest" version
// and a fake igor binary printing "[]", so the Composer wrapper runs without network access.
func setupWrapperSandbox(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("skipping: the fake igor binary is a shell script")
	}
	requirePHP(t)

	sandbox := t.TempDir()
	wrapper, err := os.ReadFile(filepath.Join("..", "..", "bin", "igor-php"))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(sandbox, "bin", "igor-php"), string(wrapper))

	binDir := filepath.Join(sandbox, "resources", "bin")
	writeTestFile(t, filepath.Join(binDir, ".latest-version"), "9.9.9")
	fakeBinary := filepath.Join(binDir, "9-9-9_"+runtime.GOOS+"_"+runtime.GOARCH, "igor-php")
	writeTestFile(t, fakeBinary, "#!/bin/sh\necho '[]'\n")
	if err := os.Chmod(fakeBinary, 0755); err != nil {
		t.Fatal(err)
	}
	return sandbox
}

func runWrapper(t *testing.T, sandbox string, env ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command("php", filepath.Join(sandbox, "bin", "igor-php"), "--output", "json", ".")
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "IGOR_VERSION=") && !strings.HasPrefix(kv, "IGOR_AUTOLOAD_LOCATION=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, env...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	exitCode := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("failed to run wrapper: %v", err)
		}
		exitCode = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), exitCode
}

func TestBinWrapper_InfoMessagesDoNotPolluteStdout(t *testing.T) {
	sandbox := setupWrapperSandbox(t)

	// No resolvable version: the wrapper falls back to the cached latest version and says so
	stdout, stderr, exitCode := runWrapper(t, sandbox)
	if exitCode != 0 {
		t.Fatalf("Expected exit code 0, got %d (stderr: %s)", exitCode, stderr)
	}
	if stdout != "[]\n" {
		t.Errorf("Expected stdout to contain only the binary output, got %q", stdout)
	}
	if !strings.Contains(stderr, "Falling back to latest version") {
		t.Errorf("Expected the fallback message on stderr, got %q", stderr)
	}
}

func TestBinWrapper_ErrorsExitWithFailure(t *testing.T) {
	sandbox := setupWrapperSandbox(t)

	stdout, stderr, exitCode := runWrapper(t, sandbox, "IGOR_VERSION=bad/version")
	if exitCode != 1 {
		t.Errorf("Expected exit code 1 for an invalid version, got %d", exitCode)
	}
	if stdout != "" {
		t.Errorf("Expected nothing on stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "Invalid version format") {
		t.Errorf("Expected the error message on stderr, got %q", stderr)
	}
}
