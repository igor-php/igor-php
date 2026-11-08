package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/igor-php/igor-php/internal/analyzer"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/reporter"
	"github.com/igor-php/igor-php/pkg/symbol"
)

func findingMessages(results []symbol.AuditStatus) map[string][]string {
	messages := map[string][]string{}
	for _, res := range results {
		for _, f := range res.Findings {
			messages[filepath.Base(res.FilePath)] = append(messages[filepath.Base(res.FilePath)], f.Message)
		}
	}
	return messages
}

// TestAuditProjectReusedAcrossAudits checks that a loaded project can be audited many times, as an
// MCP server does: each audit gives the result of a fresh CLI run, and an unsaved file content
// passed to one audit does not leak into the next one.
func TestAuditProjectReusedAcrossAudits(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"src/Cart.php":  "<?php\nclass Cart {\n    private array $lines = [];\n    public function add(string $l): void { $this->lines[] = $l; }\n}\n",
		"src/Clock.php": "<?php\nclass Clock {\n    public function now(): void { date_default_timezone_set('UTC'); }\n}\n",
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
	cfg.OutputFormat = "json"
	cfg.NoAgent = true

	project, err := loadAuditProject(root, cfg)
	if err != nil {
		t.Fatal(err)
	}

	cliCfg := cfg
	want, _, err := runAudit(&cliCfg, root, reporter.NewJSONReporter())
	if err != nil {
		t.Fatal(err)
	}
	if len(findingMessages(want)) != 2 {
		t.Fatalf("expected findings in both files, got %v", findingMessages(want))
	}

	first := cfg
	collected := 0
	got, _ := project.audit(&first, func(n int) { collected = n })
	if collected != 2 || !reflect.DeepEqual(findingMessages(got), findingMessages(want)) {
		t.Fatalf("first audit: %d files, %v, want %v", collected, findingMessages(got), findingMessages(want))
	}

	// One file, as saved: only its finding
	onDisk := cfg
	onDisk.TargetFile = filepath.Join(root, "src/Cart.php")
	got, _ = project.audit(&onDisk, nil)
	if msgs := findingMessages(got); len(msgs) != 1 || len(msgs["Cart.php"]) != 1 {
		t.Fatalf("expected the finding of Cart.php only, got %v", msgs)
	}

	// One file with unsaved content that fixes the leak
	single := cfg
	single.TargetFile = filepath.Join(root, "src/Cart.php")
	single.StdinContent = []byte("<?php\nclass Cart {\n    public function add(string $l): string { return $l; }\n}\n")
	got, _ = project.audit(&single, nil)
	if msgs := findingMessages(got); len(msgs) != 0 {
		t.Fatalf("the unsaved content has no leak, got %v", msgs)
	}

	// The next whole-project audit reads the files on disk again
	again := cfg
	got, _ = project.audit(&again, nil)
	if !reflect.DeepEqual(findingMessages(got), findingMessages(want)) {
		t.Fatalf("audit after a single-file audit: %v, want %v", findingMessages(got), findingMessages(want))
	}
}

// TestAuditProjectReusesContainerDump checks that the container dump is read once, when the
// project is loaded: later audits keep skipping the transient services and keep the aliases,
// even after the dump file is gone, and an audit does not alter the retained maps.
func TestAuditProjectReusesContainerDump(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"src/Cart.php":            "<?php\nnamespace App;\nclass Cart {\n    private array $lines = [];\n    public function add(string $l): void { $this->lines[] = $l; }\n}\n",
		"src/Basket.php":          "<?php\nnamespace App;\nclass Basket {\n    private array $items = [];\n    public function add(string $i): void { $this->items[] = $i; }\n}\n",
		"src/BasketInterface.php": "<?php\nnamespace App;\ninterface BasketInterface {}\n",
		"igor-container.json":     `{"services":[{"class":"App\\Cart","shared":false},{"class":"App\\Basket","shared":true}],"aliases":{"App\\BasketInterface":"App\\Basket"}}`,
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
	cfg.OutputFormat = "json"
	cfg.NoAgent = true
	cfg.ContainerDump = "igor-container.json"

	project, err := loadAuditProject(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	wantNonShared := analyzer.NonSharedServiceMap{"App\\Cart": true}
	wantAliases := analyzer.AliasesMap{"App\\BasketInterface": "App\\Basket"}
	if !reflect.DeepEqual(project.nonShared, wantNonShared) || !reflect.DeepEqual(project.aliases, wantAliases) {
		t.Fatalf("loaded maps: %v %v", project.nonShared, project.aliases)
	}

	// The dump is not read again by the audits
	if err := os.Remove(filepath.Join(root, "igor-container.json")); err != nil {
		t.Fatal(err)
	}

	assertOnlyBasket := func(step string, results []symbol.AuditStatus) {
		t.Helper()
		msgs := findingMessages(results)
		if len(msgs["Cart.php"]) != 0 || len(msgs["Basket.php"]) == 0 {
			t.Fatalf("%s: the transient Cart must be skipped and Basket flagged, got %v", step, msgs)
		}
	}

	whole := cfg
	got, _ := project.audit(&whole, nil)
	assertOnlyBasket("whole project", got)

	onDisk := cfg
	onDisk.TargetFile = filepath.Join(root, "src/Cart.php")
	got, _ = project.audit(&onDisk, nil)
	if msgs := findingMessages(got); len(msgs) != 0 {
		t.Fatalf("the transient Cart audited alone must stay skipped, got %v", msgs)
	}

	unsaved := cfg
	unsaved.TargetFile = filepath.Join(root, "src/Basket.php")
	unsaved.StdinContent = []byte("<?php\nnamespace App;\nclass Basket {\n    public function add(string $i): string { return $i; }\n}\n")
	got, _ = project.audit(&unsaved, nil)
	if msgs := findingMessages(got); len(msgs) != 0 {
		t.Fatalf("the unsaved Basket has no leak, got %v", msgs)
	}

	again := cfg
	got, _ = project.audit(&again, nil)
	assertOnlyBasket("whole project again", got)

	if !reflect.DeepEqual(project.nonShared, wantNonShared) || !reflect.DeepEqual(project.aliases, wantAliases) {
		t.Fatalf("the audits changed the retained maps: %v %v", project.nonShared, project.aliases)
	}
}
