package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

func stripANSI(str string) string {
	const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[-a-zA-Z\\d\\/#&.:=?%@~]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PR-TZcf-ntqry=><~]))"
	re := regexp.MustCompile(ansi)
	return re.ReplaceAllString(str, "")
}

func TestReporter_PrintFindings(t *testing.T) {
	r := NewReporter()
	projectRoot := "/tmp/project"

	tests := []struct {
		name        string
		res         AuditStatus
		expected    []string
		notExpected []string
	}{
		{
			name: "Project file finding",
			res: AuditStatus{
				ServiceID: "app.service",
				FilePath:  "/tmp/project/src/Service.php",
				Findings: []Finding{
					{
						Message:     "State mutation",
						Code:        "$this->state = 1;",
						Remediation: "Refactor me",
						Severity:    "ERROR",
						Line:        10,
					},
				},
			},
			expected: []string{
				"[PROJECT]",
				"📂 src/Service.php",
				"Service: app.service",
				"State mutation",
				"10 | $this->state = 1;",
				"💡 Hint: Refactor me",
			},
			notExpected: []string{
				"Since this is your code",
			},
		},
		{
			name: "Vendor file finding",
			res: AuditStatus{
				ServiceID: "vendor.service",
				FilePath:  "/tmp/project/vendor/bundle/Service.php",
				Findings: []Finding{
					{
						Message:     "State mutation in vendor",
						Code:        "self::$cache = [];",
						Remediation: "",
						Severity:    "ERROR",
						Line:        20,
					},
				},
			},
			expected: []string{
				"[VENDOR]",
				"📂 vendor/bundle/Service.php",
				"Service: vendor.service",
				"State mutation in vendor",
				"20 | self::$cache = [];",
			},
			notExpected: []string{
				"This is third-party code",
				"max_requests",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			old := os.Stdout
			r_out, w_out, _ := os.Pipe()
			os.Stdout = w_out

			r.PrintFindings(tt.res, projectRoot, tt.res.IsVendor(projectRoot))

			_ = w_out.Close()
			os.Stdout = old

			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r_out)
			output := stripANSI(buf.String())

			for _, exp := range tt.expected {
				if !strings.Contains(output, exp) {
					t.Errorf("Expected output to contain %q, but it didn't.\nOutput:\n%s", exp, output)
				}
			}
			for _, nexp := range tt.notExpected {
				if strings.Contains(output, nexp) {
					t.Errorf("Expected output NOT to contain %q, but it did.\nOutput:\n%s", nexp, output)
				}
			}
		})
	}
}

func TestReporter_PrintSummary(t *testing.T) {
	r := NewReporter()
	projectRoot := "/tmp/project"

	results := []AuditStatus{
		{
			FilePath: "/tmp/project/src/Service.php", // Project
			Status:   "❌ KO",
		},
		{
			FilePath: "/tmp/project/vendor/Bundle.php", // Vendor
			Status:   "❌ KO",
		},
		{
			FilePath: "/tmp/project/src/Safe.php",
			Status:   "✅ OK",
		},
	}

	// Capture stdout
	old := os.Stdout
	r_out, w_out, _ := os.Pipe()
	os.Stdout = w_out

	r.PrintSummary(results, projectRoot)

	_ = w_out.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r_out)
	output := stripANSI(buf.String())

	expected := []string{
		"💡 RECOMMENDATIONS:",
		"[PROJECT] Since this is your code",
		"[VENDOR]  This is third-party code",
		"max_requests",
		"❌ KO (Dangerous State):     2 (Project: 1, Vendor: 1)",
	}

	for _, exp := range expected {
		if !strings.Contains(output, exp) {
			t.Errorf("Expected summary to contain %q, but it didn't.\nOutput:\n%s", exp, output)
		}
	}
}
