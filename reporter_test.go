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
		name     string
		res      AuditStatus
		expected []string
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
				"💡 Hint: Since this is your code, you should refactor this service to be stateless",
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
				"💡 Hint: This is third-party code",
				"max_requests",
				"Determine whether the issue is simply a memory leak",
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

			w_out.Close()
			os.Stdout = old

			var buf bytes.Buffer
			io.Copy(&buf, r_out)
			output := stripANSI(buf.String())

			for _, exp := range tt.expected {
				if !strings.Contains(output, exp) {
					t.Errorf("Expected output to contain %q, but it didn't.\nOutput:\n%s", exp, output)
				}
			}
		})
	}
}
