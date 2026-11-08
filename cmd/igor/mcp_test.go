package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/igor-php/igor-php/internal/auditor"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// mcpTestProject writes a plain PHP project (no framework) with a leaking Cart, a process-state Clock and a
// clean Price, then connects an MCP client to an igor server for it.
func mcpTestProject(t *testing.T) (string, *mcp.ClientSession) {
	t.Helper()
	return mcpTestProjectWith(t, false)
}

func mcpTestProjectWith(t *testing.T, readOnly bool) (string, *mcp.ClientSession) {
	t.Helper()
	t.Setenv(noUpdateCheckEnv, "1")
	root := t.TempDir()
	files := map[string]string{
		"composer.json":    `{"require": {"php": ">=8.2"}, "require-dev": {"igor-php/igor-php": "^0.10"}}`,
		"src/Cart.php":     "<?php\nclass Cart {\n    private array $lines = [];\n    public function add(string $l): void { $this->lines[] = $l; }\n}\n",
		"src/Clock.php":    "<?php\nclass Clock {\n    public function now(): void { date_default_timezone_set('UTC'); }\n}\n",
		"src/Price.php":    "<?php\nclass Price {\n    public function __construct(private int $amount) {}\n    public function amount(): int { return $this->amount; }\n}\n",
		"lib/Registry.php": "<?php\nclass Registry {\n    private static array $items = [];\n    public static function add(string $i): void { self::$items[] = $i; }\n}\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.LoadConfig(root, "")
	cfg.NoAgent = true
	server := newMCPServer(root, cfg)
	server.checker.cacheFile = ""
	server.readOnly = readOnly

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.server().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		_ = serverSession.Wait()
	})
	return root, session
}

// callTool calls a tool and decodes its structured result into out. It fails the test on a tool error
// unless wantErr is set, and returns the text of the error.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, out any) string {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		text := ""
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text += tc.Text
			}
		}
		if out != nil {
			t.Fatalf("%s failed: %s", name, text)
		}
		return text
	}
	if out != nil {
		data, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s: %v in %s", name, err, data)
		}
	}
	return ""
}

func TestMCPListsTools(t *testing.T) {
	writers := map[string]bool{"baseline_add": true, "baseline_prune": true}
	for _, readOnly := range []bool{false, true} {
		_, session := mcpTestProjectWith(t, readOnly)
		res, err := session.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
			if tool.Annotations == nil || tool.Annotations.ReadOnlyHint == writers[tool.Name] {
				t.Errorf("%s: wrong read-only hint", tool.Name)
			}
			if tool.Description == "" {
				t.Errorf("%s has no description", tool.Name)
			}
		}
		want := "audit_file,audit_project,baseline_add,baseline_check,baseline_prune,explain_service,get_rule,project_info"
		if readOnly {
			want = "audit_file,audit_project,baseline_check,explain_service,get_rule,project_info"
		}
		if strings.Join(names, ",") != want {
			t.Fatalf("read-only %v: tools %v, want %s", readOnly, names, want)
		}
	}
}

func TestMCPProjectInfo(t *testing.T) {
	root, session := mcpTestProject(t)
	var info projectInfoOutput
	callTool(t, session, "project_info", nil, &info)
	if info.Root != root || info.Framework != "none" || info.DeepAudit || info.Octane {
		t.Fatalf("unexpected project info: %+v", info)
	}
	if info.Igor.Version != Version || info.Igor.Install != "composer" || info.Igor.Constraint != "^0.10" {
		t.Fatalf("unexpected igor info: %+v", info.Igor)
	}
	if !strings.Contains(info.Igor.Note, noUpdateCheckEnv) || info.Igor.UpToDate != nil {
		t.Fatalf("the update check must be disabled: %+v", info.Igor)
	}
}

func TestMCPAuditProject(t *testing.T) {
	_, session := mcpTestProject(t)

	var all auditProjectOutput
	callTool(t, session, "audit_project", nil, &all)
	if all.FilesAudited != 4 || all.Total != 3 || all.Total != all.Errors+all.Warnings || len(all.Findings) != 3 {
		t.Fatalf("unexpected audit: %+v", all)
	}
	files := map[string]bool{}
	for _, f := range all.Findings {
		files[f.File] = true
		if f.RuleID == "" || f.Message == "" || f.Line == 0 {
			t.Errorf("incomplete finding: %+v", f)
		}
	}
	if !files["src/Cart.php"] || !files["src/Clock.php"] || !files["lib/Registry.php"] {
		t.Fatalf("expected findings in Cart, Clock and Registry, got %v", files)
	}

}

func TestMCPAuditProjectPages(t *testing.T) {
	_, session := mcpTestProject(t)
	var page auditProjectOutput
	callTool(t, session, "audit_project", map[string]any{"limit": 2}, &page)
	if len(page.Findings) != 2 || page.NextOffset != 2 || page.Total != 3 {
		t.Fatalf("first page: %+v", page)
	}
	var last auditProjectOutput
	callTool(t, session, "audit_project", map[string]any{"limit": 2, "offset": 2}, &last)
	if len(last.Findings) != 1 || last.NextOffset != 0 {
		t.Fatalf("last page: %+v", last)
	}

}

func TestMCPAuditProjectFilters(t *testing.T) {
	_, session := mcpTestProject(t)
	var all auditProjectOutput
	callTool(t, session, "audit_project", nil, &all)
	var src auditProjectOutput
	callTool(t, session, "audit_project", map[string]any{"path": "src"}, &src)
	if src.Total != 2 {
		t.Fatalf("expected the 2 findings of src/, got %+v", src)
	}
	rule := all.Findings[0].RuleID
	var byRule auditProjectOutput
	callTool(t, session, "audit_project", map[string]any{"rule_id": rule}, &byRule)
	for _, f := range byRule.Findings {
		if f.RuleID != rule {
			t.Fatalf("rule filter %s returned %+v", rule, f)
		}
	}

	if msg := callTool(t, session, "audit_project", map[string]any{"severity": "FATAL"}, nil); !strings.Contains(msg, "severity") {
		t.Fatalf("expected a severity error, got %q", msg)
	}
	if msg := callTool(t, session, "audit_project", map[string]any{"path": "../elsewhere"}, nil); !strings.Contains(msg, "outside the project root") {
		t.Fatalf("expected a path error, got %q", msg)
	}
}

func TestMCPAuditFile(t *testing.T) {
	root, session := mcpTestProject(t)

	var saved auditFileOutput
	callTool(t, session, "audit_file", map[string]any{"path": "src/Cart.php"}, &saved)
	if !saved.Audited || saved.File != "src/Cart.php" || len(saved.Findings) != 1 {
		t.Fatalf("saved Cart: %+v", saved)
	}

	// The unsaved fix is clean, and the file on disk is unchanged
	fixed := "<?php\nclass Cart {\n    public function add(string $l): string { return $l; }\n}\n"
	var unsaved auditFileOutput
	callTool(t, session, "audit_file", map[string]any{"path": filepath.Join(root, "src/Cart.php"), "content": fixed}, &unsaved)
	if !unsaved.Audited || len(unsaved.Findings) != 0 {
		t.Fatalf("unsaved fix: %+v", unsaved)
	}
	var again auditFileOutput
	callTool(t, session, "audit_file", map[string]any{"path": "src/Cart.php"}, &again)
	if len(again.Findings) != 1 {
		t.Fatalf("the unsaved content leaked into the next audit: %+v", again)
	}

	var clean auditFileOutput
	callTool(t, session, "audit_file", map[string]any{"path": "src/Price.php"}, &clean)
	if !clean.Audited || len(clean.Findings) != 0 {
		t.Fatalf("Price: %+v", clean)
	}

	outside := filepath.Join(t.TempDir(), "Evil.php")
	if err := os.WriteFile(outside, []byte("<?php\nclass Evil {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if msg := callTool(t, session, "audit_file", map[string]any{"path": outside}, nil); !strings.Contains(msg, "outside the project root") {
		t.Fatalf("expected a path error, got %q", msg)
	}
	if msg := callTool(t, session, "audit_file", map[string]any{"path": "src/Missing.php"}, nil); !strings.Contains(msg, "cannot read") {
		t.Fatalf("expected a missing file error, got %q", msg)
	}
}

func TestMCPAuditFileRefusesSymlinkOutsideRoot(t *testing.T) {
	root, session := mcpTestProject(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "Evil.php"), []byte("<?php\nclass Evil {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if msg := callTool(t, session, "audit_file", map[string]any{"path": "linked/Evil.php"}, nil); !strings.Contains(msg, "outside the project root") {
		t.Fatalf("expected a path error, got %q", msg)
	}
}

func TestMCPExplainService(t *testing.T) {
	_, session := mcpTestProject(t)
	var out explainServiceOutput
	callTool(t, session, "explain_service", map[string]any{"name": "cart"}, &out)
	if out.Matches != 1 || len(out.Services) != 1 {
		t.Fatalf("expected Cart only: %+v", out)
	}
	if svc := out.Services[0]; svc.File != "src/Cart.php" || svc.Verdict != "KO" || len(svc.Findings) != 1 {
		t.Fatalf("Cart: %+v", svc)
	}
	var price explainServiceOutput
	callTool(t, session, "explain_service", map[string]any{"name": "Price"}, &price)
	if price.Matches != 1 || price.Services[0].Verdict != "OK" {
		t.Fatalf("Price: %+v", price)
	}
}

func TestMCPGetRule(t *testing.T) {
	_, session := mcpTestProject(t)
	var list getRuleOutput
	callTool(t, session, "get_rule", nil, &list)
	if len(list.Rules) == 0 {
		t.Fatal("expected the list of rules")
	}
	var rule getRuleOutput
	callTool(t, session, "get_rule", map[string]any{"rule_id": list.Rules[0]}, &rule)
	if rule.RuleID != list.Rules[0] || rule.Doc == "" {
		t.Fatalf("rule: %+v", rule)
	}
	if msg := callTool(t, session, "get_rule", map[string]any{"rule_id": "Nope"}, nil); !strings.Contains(msg, "unknown rule") {
		t.Fatalf("expected an unknown rule error, got %q", msg)
	}
}

func TestMCPRuleResourcesAndPrompt(t *testing.T) {
	_, session := mcpTestProject(t)
	ctx := context.Background()

	index, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "igor://rules"})
	if err != nil || !strings.Contains(index.Contents[0].Text, "igor://rules/StateMutation") {
		t.Fatalf("rules index: %v %+v", err, index)
	}
	rule, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "igor://rules/StateMutation"})
	if err != nil || !strings.HasPrefix(rule.Contents[0].Text, "# StateMutation") {
		t.Fatalf("rule resource: %v %+v", err, rule)
	}
	if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "igor://rules/Nope"}); err == nil {
		t.Fatal("expected an error for an unknown rule")
	}

	prompt, err := session.GetPrompt(ctx, &mcp.GetPromptParams{Name: "audit-worker", Arguments: map[string]string{"path": "src"}})
	if err != nil {
		t.Fatal(err)
	}
	text := prompt.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(text, "project_info") || !strings.Contains(text, `audit_project with path "src"`) || !strings.Contains(text, "update_command") {
		t.Fatalf("prompt: %s", text)
	}
}

func TestMCPExplainServiceSkippedServices(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"src/Cart.php":   "<?php\nnamespace App;\nclass Cart {\n    private array $lines = [];\n    public function add(string $l): void { $this->lines[] = $l; }\n}\n",
		"src/Basket.php": "<?php\nnamespace App;\nclass Basket {\n    private array $items = [];\n    public function add(string $i): void { $this->items[] = $i; }\n}\n",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.LoadConfig(root, "")
	server := newMCPServer(root, cfg)
	bridge := auditor.NewSymfonyBridge(root, "", cfg)
	bridge.Container = &symbol.SymfonyContainer{Definitions: map[string]symbol.SymfonyService{
		"app.cart":   {Class: "App\\Cart", Shared: true},
		"app.basket": {Class: "App\\Basket", Shared: false},
		"app.ghost":  {Class: "App\\Ghost", Shared: true},
	}}
	bridge.ClassToFile = map[string]string{
		"App\\Cart":   filepath.Join(root, "src/Cart.php"),
		"App\\Basket": filepath.Join(root, "src/Basket.php"),
	}
	server.project = &auditProject{rootPath: root, bridge: bridge}

	explain := func(name string) explainServiceOutput {
		t.Helper()
		_, out, err := server.explainServiceTool(context.Background(), nil, explainServiceInput{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	if out := explain("cart"); out.Matches != 1 || out.Services[0].Verdict != "KO" {
		t.Fatalf("the shared Cart is audited: %+v", out)
	}
	// Basket is also audited as a class of the project, but its service is not shared
	var basket *explainedService
	services := explain("basket").Services
	for i := range services {
		if services[i].ServiceID == "app.basket" {
			basket = &services[i]
		}
	}
	if basket == nil || basket.Verdict != "SKIPPED" || basket.Shared || !strings.Contains(basket.Skipped, "not shared") || basket.File != "src/Basket.php" {
		t.Fatalf("the non-shared Basket must be explained as skipped: %+v", basket)
	}
	if out := explain("ghost"); out.Matches != 1 || !strings.Contains(out.Services[0].Skipped, "could not locate the file") {
		t.Fatalf("Ghost: %+v", out)
	}
}

func TestMCPFailedFilesAreReported(t *testing.T) {
	root := t.TempDir()
	server := newMCPServer(root, config.LoadConfig(root, ""))
	results := []symbol.AuditStatus{
		{FilePath: filepath.Join(root, "src/Ok.php"), Status: "✅ OK"},
		{FilePath: filepath.Join(root, "src/Broken.php"), Status: auditFailedStatus},
	}
	if failed := server.failedFiles(results); len(failed) != 1 || failed[0] != "src/Broken.php" {
		t.Fatalf("failed files: %v", failed)
	}
	if svc := server.explainAudited(results[1]); svc.Verdict != "ERROR" {
		t.Fatalf("a file that could not be audited is not clean: %+v", svc)
	}
}
