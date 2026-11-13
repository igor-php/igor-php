package igorphp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The corpus workflow relies on the exit code of scripts/corpus-report.php: with --strict, findings
// that differ from the baseline (or no baseline) fail the job; without it they are only reported,
// and --update writes the baseline instead of failing.
func TestCorpusReportExitCodes(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php is not installed")
	}
	script, err := filepath.Abs(filepath.Join("scripts", "corpus-report.php"))
	if err != nil {
		t.Fatal(err)
	}

	const results = `[{"file_path": "src/Service.php", "findings": [
		{"rule_id": "StateMutation", "severity": "ERROR"},
		{"rule_id": "StateMutation", "severity": "ERROR"}]}]`
	const same = `{"project": "app", "commit": "abc", "files": 1, "counts": {"StateMutation ERROR": 2}}`
	const changed = `{"project": "app", "commit": "abc", "files": 1, "counts": {"StateMutation ERROR": 1}}`

	for _, tc := range []struct {
		name       string
		baseline   string // "" for no baseline
		igorStatus string
		flags      []string
		want       int
		written    bool
	}{
		{name: "same, strict", baseline: same, igorStatus: "0", flags: []string{"--strict"}, want: 0},
		{name: "changed, strict", baseline: changed, igorStatus: "0", flags: []string{"--strict"}, want: 1},
		{name: "changed, not strict", baseline: changed, igorStatus: "0", want: 0},
		{name: "no baseline, strict", igorStatus: "0", flags: []string{"--strict"}, want: 1},
		{name: "changed, strict update", baseline: changed, igorStatus: "0", flags: []string{"--strict", "--update"}, want: 0, written: true},
		{name: "igor failed", baseline: same, igorStatus: "1", want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, content string) string {
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				return path
			}
			baseline := filepath.Join(dir, "baseline.json")
			if tc.baseline != "" {
				write("baseline.json", tc.baseline)
			}
			args := append([]string{script,
				"--name", "app", "--framework", "laravel", "--commit", "abc", "--dir", dir,
				"--results", write("results.json", results), "--log", write("igor.log", ""),
				"--igor-status", tc.igorStatus, "--baseline", baseline,
			}, tc.flags...)
			cmd := exec.Command(php, args...)
			// Outside GitHub Actions: no annotation, nothing appended to the job summary
			for _, kv := range os.Environ() {
				if !strings.HasPrefix(kv, "GITHUB_") {
					cmd.Env = append(cmd.Env, kv)
				}
			}
			out, _ := cmd.CombinedOutput()
			if got := cmd.ProcessState.ExitCode(); got != tc.want {
				t.Fatalf("exit code %d, want %d\n%s", got, tc.want, out)
			}
			if tc.written {
				data, err := os.ReadFile(baseline)
				if err != nil || !strings.Contains(string(data), `"StateMutation ERROR": 2`) {
					t.Fatalf("the baseline was not rewritten: %s %v", data, err)
				}
			}
		})
	}
}
