package reporter

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/pkg/symbol"
)

func stripANSI(str string) string {
	const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[-a-zA-Z\\d\\/#&.:=?%@~]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PR-TZcf-ntqry=><~]))"
	re := regexp.MustCompile(ansi)
	return re.ReplaceAllString(str, "")
}

func TestReporter_PrintFindings(t *testing.T) {
	rep := NewReporter()
	r := rep.(*CLIReporter)
	r.IsGitHub = false // Ensure standard CLI output for tests
	projectRoot := "/tmp/project"

	tests := []struct {
		name        string
		res         symbol.AuditStatus
		expected    []string
		notExpected []string
	}{
		{
			name: "Project file finding",
			res: symbol.AuditStatus{
				ServiceID: "app.service",
				FilePath:  "/tmp/project/src/Service.php",
				Findings: []symbol.Finding{
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
			res: symbol.AuditStatus{
				ServiceID: "vendor.service",
				FilePath:  "/tmp/project/vendor/bundle/Service.php",
				Findings: []symbol.Finding{
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
			rOut, wOut, _ := os.Pipe()
			os.Stdout = wOut

			r.PrintFindings(tt.res, projectRoot, tt.res.IsVendor(projectRoot))

			_ = wOut.Close()
			os.Stdout = old

			var buf bytes.Buffer
			_, _ = io.Copy(&buf, rOut)
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
	rep := NewReporter()
	r := rep.(*CLIReporter)
	r.IsGitHub = false
	projectRoot := "/tmp/project"

	results := []symbol.AuditStatus{
		{
			FilePath: "/tmp/project/src/Service.php", // Project
			Status:   "❌ KO",
		},
		{
			FilePath: "/tmp/project/vendor/Bundle.php", // Vendor
			Status:   "❌ KO",
		},
		{
			FilePath: "/tmp/project/src/WarnService.php", // Project
			Status:   "⚠️  WARN",
		},
		{
			FilePath: "/tmp/project/vendor/WarnBundle.php", // Vendor
			Status:   "⚠️  WARN",
		},
		{
			FilePath: "/tmp/project/src/Safe.php",
			Status:   "✅ OK",
		},
	}

	// Capture stdout
	old := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	r.PrintSummary(results, projectRoot)

	_ = wOut.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	output := stripANSI(buf.String())

	expected := []string{
		"💡 RECOMMENDATIONS:",
		"[PROJECT] Since this is your code",
		"[VENDOR]  This is third-party code",
		"max_requests",
		"❌ KO (Dangerous State):     2 (Project: 1, Vendor: 1)",
		"⚠️  WARN (Review reset):      2 (Project: 1, Vendor: 1)",
	}

	for _, exp := range expected {
		if !strings.Contains(output, exp) {
			t.Errorf("Expected summary to contain %q, but it didn't.\nOutput:\n%s", exp, output)
		}
	}

	// Congratulation test
	resultsAllSafe := []symbol.AuditStatus{
		{
			FilePath: "/tmp/project/src/Safe.php",
			Status:   "✅ OK",
		},
	}

	rOut3, wOut3, _ := os.Pipe()
	os.Stdout = wOut3

	r.PrintSummary(resultsAllSafe, projectRoot)

	_ = wOut3.Close()
	os.Stdout = old

	var buf3 bytes.Buffer
	_, _ = io.Copy(&buf3, rOut3)
	output3 := stripANSI(buf3.String())

	if !strings.Contains(output3, "CONGRATULATIONS: Your application and all its dependencies are compatible") {
		t.Errorf("Expected congratulation message, got: %q", output3)
	}
}

func TestJSONReporter(t *testing.T) {
	rep := NewJSONReporter()
	r := rep.(*JSONReporter)
	projectRoot := "/tmp/project"

	res := symbol.AuditStatus{
		ServiceID: "app.service",
		FilePath:  "/tmp/project/src/Service.php",
		Findings: []symbol.Finding{
			{
				Message:  "State mutation",
				Severity: "ERROR",
				Line:     10,
			},
		},
	}

	r.PrintFindings(res, projectRoot, false)

	if len(r.Findings) != 1 {
		t.Fatalf("Expected 1 finding, got %d", len(r.Findings))
	}

	// Capture stdout for Summary
	old := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	r.PrintSummary([]symbol.AuditStatus{res}, projectRoot)

	_ = wOut.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	output := buf.String()

	// Verify valid JSON
	if !strings.Contains(output, "\"file_path\": \"/tmp/project/src/Service.php\"") {
		t.Errorf("Expected JSON to contain file_path, but it didn't.\nOutput:\n%s", output)
	}
	if !strings.Contains(output, "\"message\": \"State mutation\"") {
		t.Errorf("Expected JSON to contain message, but it didn't.\nOutput:\n%s", output)
	}
}

func TestReporter_Headers(t *testing.T) {
	// 1. CLIReporter
	rep := NewReporter()
	r := rep.(*CLIReporter)

	// Capture stdout
	old := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	r.PrintHeader(42)
	r.PrintProjectHeader()
	r.PrintVendorHeader()

	_ = wOut.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	output := stripANSI(buf.String())

	if !strings.Contains(output, "Igor is auditing 42 unique shared service files") {
		t.Errorf("Expected output to contain audit count header, got: %q", output)
	}
	if !strings.Contains(output, "PROJECT SERVICES") {
		t.Errorf("Expected output to contain PROJECT SERVICES, got: %q", output)
	}
	if !strings.Contains(output, "VENDOR SERVICES") {
		t.Errorf("Expected output to contain VENDOR SERVICES, got: %q", output)
	}

	// 2. JSONReporter (headers should be safe no-ops)
	jsonRep := NewJSONReporter()
	jr := jsonRep.(*JSONReporter)
	jr.PrintHeader(42)
	jr.PrintProjectHeader()
	jr.PrintVendorHeader()
}

func TestReporter_PrintFindings_GitHub(t *testing.T) {
	rep := NewReporter()
	r := rep.(*CLIReporter)
	r.IsGitHub = true
	projectRoot := "/tmp/project"

	res := symbol.AuditStatus{
		ServiceID: "app.service",
		FilePath:  "/tmp/project/src/Service.php",
		Findings: []symbol.Finding{
			{
				Message:     "State mutation",
				Code:        "$this->state = 1;",
				Remediation: "Refactor me",
				Severity:    "ERROR",
				Line:        10,
			},
		},
	}

	// Capture stdout
	old := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	r.PrintFindings(res, projectRoot, false)

	_ = wOut.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	output := stripANSI(buf.String())

	if !strings.Contains(output, "::error file=src/Service.php,line=10::[Igor] State mutation %0A 💡 Hint: Refactor me %0A 💡 Hint: Since this is your code, you should refactor this service to be stateless") {
		t.Errorf("Expected GitHub actions error annotation, got: %q", output)
	}

	// Vendor warning
	resVendor := symbol.AuditStatus{
		ServiceID: "vendor.service",
		FilePath:  "/tmp/project/vendor/bundle/Service.php",
		Findings: []symbol.Finding{
			{
				Rule:        symbol.RuleStateMutation,
				Message:     "State mutation in vendor",
				Code:        "self::$cache = [];",
				Remediation: "",
				Severity:    "WARNING",
				Line:        20,
			},
		},
	}

	rOut2, wOut2, _ := os.Pipe()
	os.Stdout = wOut2

	r.PrintFindings(resVendor, projectRoot, true)

	_ = wOut2.Close()
	os.Stdout = old

	var buf2 bytes.Buffer
	_, _ = io.Copy(&buf2, rOut2)
	output2 := stripANSI(buf2.String())

	if !strings.Contains(output2, "::warning file=vendor/bundle/Service.php,line=20,title=Igor StateMutation::[Igor] State mutation in vendor %0A 💡 Hint: This is third-party code.") {
		t.Errorf("Expected GitHub actions warning annotation, got: %q", output2)
	}
}

func TestReporter_PrintFindings_IgnoredFinding(t *testing.T) {
	rep := NewReporter()
	r := rep.(*CLIReporter)
	r.IsGitHub = true
	projectRoot := "/tmp/project"

	res := symbol.AuditStatus{
		ServiceID: "app.service",
		FilePath:  "/tmp/project/src/Service.php",
		Findings: []symbol.Finding{
			{
				Message:      "Legacy state mutation",
				Code:         "$this->state = 1;",
				Remediation:  "Refactor me",
				Severity:     "ERROR",
				Line:         10,
				Ignored:      true,
				IgnoreReason: "Reset by kernel.reset",
			},
		},
	}

	old := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	r.PrintFindings(res, projectRoot, false)

	_ = wOut.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	output := stripANSI(buf.String())

	if !strings.Contains(output, "[IGNORED] Legacy state mutation") {
		t.Errorf("Expected ignored finding to be tagged [IGNORED], got: %q", output)
	}
	if !strings.Contains(output, "Baseline reason: Reset by kernel.reset") {
		t.Errorf("Expected baseline reason to be displayed, got: %q", output)
	}
	if strings.Contains(output, "::error") || strings.Contains(output, "::warning") {
		t.Errorf("Expected no GitHub annotation for ignored finding, got: %q", output)
	}
}

func capturePrintFindings(t *testing.T, r *CLIReporter, res symbol.AuditStatus, isVendor bool) string {
	t.Helper()
	old := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	r.PrintFindings(res, "/tmp/project", isVendor)
	_ = wOut.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rOut)
	return stripANSI(buf.String())
}

func TestReporter_PrintFindings_GroupsNotResetAndCaches(t *testing.T) {
	notReset := func(prop, method string, line int) symbol.Finding {
		return symbol.Finding{
			Message:       "Property '" + prop + "' of Environment is mutated but not reset in reset().",
			Severity:      "WARNING",
			Line:          line,
			ContextClass:  `Twig\Environment`,
			ContextMethod: method,
			Category:      symbol.CategoryNotReset,
			Property:      prop,
		}
	}
	res := symbol.AuditStatus{
		ServiceID: "twig",
		FilePath:  "/tmp/project/vendor/twig/twig/src/Environment.php",
		Findings: []symbol.Finding{
			notReset("globals", "addGlobal", 12),
			{Message: "Mutation of state 'count' in Environment::render()", Code: "$this->count++;", Severity: "ERROR", Line: 30},
			notReset("loadedTemplates", "load", 40),
			{
				Message: "Mutation of state 'normalizerCache' in Serializer::getNormalizer()", Code: "$this->normalizerCache[$format][$type] = $n;",
				Severity: "ERROR", Line: 50, ContextClass: `Symfony\Component\Serializer\Serializer`,
				Category: symbol.CategoryCache, Property: "normalizerCache", CacheKey: "[$format][$type]",
			},
		},
	}

	r := &CLIReporter{IsGitHub: true}
	output := capturePrintFindings(t, r, res, true)

	for _, want := range []string{
		"[VENDOR] Mutation of state 'count' in Environment::render()",
		`[VENDOR] ⚠️  Twig\Environment (resettable) — 2 properties mutated but not reset in reset():`,
		"      globals (addGlobal, line 12), loadedTemplates (load, line 40)",
		"💡 Hint: Clear these properties in the reset() method.",
		`[VENDOR] 🗄️  Cache  Symfony\Component\Serializer\Serializer::$normalizerCache  key: [$format][$type]`,
		"50 | $this->normalizerCache[$format][$type] = $n;",
		// GitHub annotations stay one per finding
		"::warning file=vendor/twig/twig/src/Environment.php,line=12::[Igor] Property 'globals'",
		"::warning file=vendor/twig/twig/src/Environment.php,line=40::[Igor] Property 'loadedTemplates'",
		"::error file=vendor/twig/twig/src/Environment.php,line=50::[Igor] Mutation of state 'normalizerCache'",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output is missing %q:\n%s", want, output)
		}
	}
	if strings.Count(output, "mutated but not reset in reset().\n") != 0 {
		t.Errorf("not-reset findings must be grouped, not listed one by one:\n%s", output)
	}
	// Regular findings first, then the group, then the caches
	count := strings.Index(output, "Mutation of state 'count'")
	group := strings.Index(output, "(resettable)")
	cache := strings.Index(output, "🗄️")
	if count >= group || group >= cache {
		t.Errorf("unexpected order (finding %d, group %d, cache %d):\n%s", count, group, cache, output)
	}

	one := capturePrintFindings(t, &CLIReporter{}, symbol.AuditStatus{FilePath: "/tmp/project/src/S.php", Findings: []symbol.Finding{notReset("only", "", 3)}}, false)
	if !strings.Contains(one, "1 property mutated but not reset in reset():") || !strings.Contains(one, "      only (line 3)") {
		t.Errorf("unexpected single-property group:\n%s", one)
	}

	// Properties with different reachability keep their own tags
	high, info := notReset("globals", "addGlobal", 12), notReset("loadedTemplates", "load", 40)
	high.Reachability, info.Reachability = "HIGH", "INFO"
	mixed := capturePrintFindings(t, &CLIReporter{}, symbol.AuditStatus{FilePath: "/tmp/project/src/S.php", Findings: []symbol.Finding{high, info}}, false)
	if !strings.Contains(mixed, "[PROJECT] ⚠️  Twig\\Environment (resettable)") || !strings.Contains(mixed, "globals (addGlobal, line 12) [HIGH], loadedTemplates (load, line 40) [INFO]") {
		t.Errorf("mixed reachability must be shown per property:\n%s", mixed)
	}
	info.Reachability = "HIGH"
	same := capturePrintFindings(t, &CLIReporter{}, symbol.AuditStatus{FilePath: "/tmp/project/src/S.php", Findings: []symbol.Finding{high, info}}, false)
	if !strings.Contains(same, "[HIGH] ⚠️  Twig\\Environment (resettable)") || strings.Count(same, "[HIGH]") != 1 {
		t.Errorf("a shared tag must be shown once on the class:\n%s", same)
	}
}

func TestWrapList(t *testing.T) {
	got := wrapList([]string{"aaaa", "bbbb", "cccc"}, 11)
	want := []string{"aaaa, bbbb,", "cccc"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("wrapList = %q, want %q", got, want)
	}
}
