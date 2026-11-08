package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The Claude Code plugin lives in plugins/igor and is published by the marketplace at the repository root.
const pluginDir = "../../plugins/igor"

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}
}

// frontmatter returns the YAML frontmatter keys of a Markdown file (one level, "key: value").
func frontmatter(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	// Git may check the file out with CRLF line endings (Windows).
	parts := strings.SplitN(strings.ReplaceAll(string(data), "\r\n", "\n"), "---\n", 3)
	if len(parts) < 3 || parts[0] != "" {
		t.Fatalf("%s has no frontmatter", path)
	}
	keys := map[string]string{}
	for _, line := range strings.Split(parts[1], "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(line, " ") {
			keys[key] = strings.TrimSpace(value)
		}
	}
	return keys
}

func TestAgentPluginManifests(t *testing.T) {
	var marketplace struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"plugins"`
	}
	readJSONFile(t, "../../.claude-plugin/marketplace.json", &marketplace)
	if len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Name != "igor" || marketplace.Plugins[0].Source != "./plugins/igor" {
		t.Fatalf("the marketplace must publish the igor plugin from ./plugins/igor, got %+v", marketplace.Plugins)
	}

	var manifest struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description"`
	}
	readJSONFile(t, filepath.Join(pluginDir, ".claude-plugin/plugin.json"), &manifest)
	if manifest.Name != "igor" || manifest.Version == "" || manifest.Description == "" {
		t.Fatalf("plugin.json needs name igor, a version and a description, got %+v", manifest)
	}

	var hooks struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	readJSONFile(t, filepath.Join(pluginDir, "hooks/hooks.json"), &hooks)
	post := hooks.Hooks["PostToolUse"]
	if len(post) != 1 || post[0].Matcher != "Edit|Write|MultiEdit" || !strings.Contains(post[0].Hooks[0].Command, "${CLAUDE_PLUGIN_ROOT}/hooks/igor-check.php") {
		t.Fatalf("unexpected PostToolUse hook: %+v", post)
	}
}

func TestAgentPluginMarkdownFiles(t *testing.T) {
	skillDir := filepath.Join(pluginDir, "skills/frankenphp-worker-audit")
	skill := frontmatter(t, filepath.Join(skillDir, "SKILL.md"))
	if skill["name"] != "frankenphp-worker-audit" || skill["description"] == "" {
		t.Fatalf("SKILL.md needs its directory name and a description, got %v", skill)
	}

	// Every reference the skill points to must exist.
	body, _ := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	for _, ref := range regexp.MustCompile("references/[a-z-]+\\.md").FindAllString(string(body), -1) {
		if _, err := os.Stat(filepath.Join(skillDir, ref)); err != nil {
			t.Errorf("SKILL.md points to %s, which does not exist", ref)
		}
	}

	for _, pattern := range []string{"commands/*.md", "agents/*.md"} {
		files, _ := filepath.Glob(filepath.Join(pluginDir, pattern))
		if len(files) == 0 {
			t.Fatalf("no %s in the plugin", pattern)
		}
		for _, file := range files {
			if frontmatter(t, file)["description"] == "" {
				t.Errorf("%s has no description", file)
			}
		}
	}
}

// TestAgentPluginCoversEveryRule keeps the skill's rule reference in sync with the rules catalog of the README.
func TestAgentPluginCoversEveryRule(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	rules, err := os.ReadFile(filepath.Join(pluginDir, "skills/frankenphp-worker-audit/references/rules.md"))
	if err != nil {
		t.Fatal(err)
	}

	catalog := regexp.MustCompile("(?m)^\\| \\*\\*[^|]+\\*\\* \\| `([A-Za-z]+)` \\|").FindAllStringSubmatch(string(readme), -1)
	if len(catalog) < 10 {
		t.Fatalf("expected the README rules catalog, found %d rules", len(catalog))
	}
	for _, rule := range catalog {
		if !regexp.MustCompile("(?m)^## " + rule[1] + "\\b").Match(rules) {
			t.Errorf("rule %s of the README has no section in references/rules.md", rule[1])
		}
	}
}

// runIgorCheckHook runs the PostToolUse hook with a fake igor-php (a PHP script, so it runs on every OS)
// that prints the given audit, only when called with --no-agent if noAgentOnly is set. It returns the
// context the hook hands to Claude ("" when it stays silent).
func runIgorCheckHook(t *testing.T, project string, payload map[string]any, audit string, noAgentOnly bool, env ...string) string {
	t.Helper()
	fake := filepath.Join(t.TempDir(), "igor-fake.php")
	script := "<?php\n"
	if noAgentOnly {
		script += "if (!in_array('--no-agent', $argv, true)) { fwrite(STDERR, 'could not load symfony container'); exit(1); }\n"
	}
	// 1 MB on stderr, more than a pipe buffer: the hook must not wait on it.
	script += "fwrite(STDERR, str_repeat('igor:dump error ', 65536));\n"
	script += "echo base64_decode('" + base64.StdEncoding.EncodeToString([]byte(audit)) + "');\n"
	if err := os.WriteFile(fake, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(payload)
	cmd := exec.Command("php", filepath.Join(pluginDir, "hooks/igor-check.php"))
	cmd.Stdin = strings.NewReader(string(input))
	cmd.Env = append(os.Environ(), append([]string{"IGOR_BIN=" + fake, "CLAUDE_PROJECT_DIR=" + project, "IGOR_WATCH="}, env...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hook failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) == "" {
		return ""
	}

	var result struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("hook output is not JSON: %v\n%s", err, out)
	}
	if result.HookSpecificOutput.HookEventName != "PostToolUse" {
		t.Fatalf("unexpected hook output: %s", out)
	}
	return result.HookSpecificOutput.AdditionalContext
}

// hookProject creates a project with a leaking service and the same file under vendor/.
func hookProject(t *testing.T) (project, service, vendorFile, source string) {
	t.Helper()
	project = t.TempDir()
	service = filepath.Join(project, "src/Service/Cart.php")
	vendorFile = filepath.Join(project, "vendor/acme/lib/Cart.php")
	readme := filepath.Join(project, "README.md")
	source = "<?php\nclass Cart {\n    private array $lines = [];\n    public function add(string $l): void { $this->lines[] = $l; }\n    public function count(): int { return count($this->lines); }\n}\n"
	for _, file := range []string{service, vendorFile, readme} {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return project, service, vendorFile, source
}

func TestAgentPluginHook(t *testing.T) {
	requirePHP(t)

	if out, err := exec.Command("php", "-l", filepath.Join(pluginDir, "hooks/igor-check.php")).CombinedOutput(); err != nil {
		t.Fatalf("PHP syntax check failed:\n%s", out)
	}

	project, service, vendorFile, source := hookProject(t)
	auditJSON, err := json.Marshal([]map[string]any{{"file_path": service, "findings": []map[string]any{
		{"rule_id": "StateMutation", "message": "Mutation of state 'lines' in Cart::add()", "remediation": "State mutations persist across requests in Worker mode.", "severity": "ERROR", "line": 4},
		{"message": "Baselined finding", "remediation": "", "severity": "ERROR", "line": 4, "ignored": true},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	audit := string(auditJSON)
	edit := func(file, newString string) map[string]any {
		return map[string]any{"cwd": project, "tool_name": "Edit", "tool_input": map[string]any{"file_path": file, "old_string": "x", "new_string": newString}}
	}
	leakingLine := "$this->lines[] = $l;"
	report := "line 4 [ERROR StateMutation] Mutation of state 'lines' in Cart::add()"

	cases := []struct {
		name    string
		watch   string // value of IGOR_WATCH; empty: the .claude/igor-watch file decides
		payload map[string]any
		noAgent bool   // the fake igor only answers with --no-agent, like a Symfony project whose cache is not warmed up
		want    string // expected substring of the context; empty: the hook stays silent
	}{
		{"reports the findings on the edited lines", "", edit(service, leakingLine), false, "1 finding(s) on the lines you just changed in src/Service/Cart.php:\n- " + report},
		{"silent when the edited lines have no finding", "", edit(service, "return count($this->lines);"), false, ""},
		{"a written file counts as fully changed", "", map[string]any{"cwd": project, "tool_name": "Write", "tool_input": map[string]any{"file_path": service, "content": source}}, false, report},
		{"MultiEdit reports the lines of every edit", "", map[string]any{"cwd": project, "tool_name": "MultiEdit", "tool_input": map[string]any{"file_path": service, "edits": []map[string]string{
			{"old_string": "a", "new_string": "return count($this->lines);"},
			{"old_string": "b", "new_string": leakingLine},
		}}}, false, report},
		{"the diff of the tool result wins over a text search", "", map[string]any{"cwd": project, "tool_name": "Edit",
			"tool_input":    map[string]any{"file_path": service, "old_string": "x", "new_string": leakingLine},
			"tool_response": map[string]any{"structuredPatch": []map[string]any{{"newStart": 2, "lines": []string{" class Cart {", "-old", "+    private array $lines = [];", "     public function add"}}}},
		}, false, ""},
		{"the diff of the tool result selects the added lines", "", map[string]any{"cwd": project, "tool_name": "Edit",
			"tool_input":    map[string]any{"file_path": service, "old_string": "x", "new_string": "class Cart {"},
			"tool_response": map[string]any{"structuredPatch": []map[string]any{{"newStart": 3, "lines": []string{"     private array $lines = [];", "-old", "-old", "+    public function add", "     public function count"}}}},
		}, false, report},
		{"falls back to --no-agent when the deep audit cannot start", "", edit(service, leakingLine), true, report},
		{"skips vendor files", "", edit(vendorFile, leakingLine), false, ""},
		{"skips non-PHP files", "", edit(filepath.Join(project, "README.md"), leakingLine), false, ""},
		{"IGOR_WATCH=0 overrides the watch file", "0", edit(service, leakingLine), false, ""},
	}

	if got := runIgorCheckHook(t, project, edit(service, leakingLine), audit, false); got != "" {
		t.Fatalf("the hook must stay silent while the watch is off, got %q", got)
	}
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".claude/igor-watch"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runIgorCheckHook(t, project, tc.payload, audit, tc.noAgent, "IGOR_WATCH="+tc.watch)
			if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
				t.Fatalf("want context containing %q, got %q", tc.want, got)
			}
			if strings.Contains(got, "Baselined finding") {
				t.Fatalf("baseline-ignored findings must not be reported: %q", got)
			}
		})
	}
}
