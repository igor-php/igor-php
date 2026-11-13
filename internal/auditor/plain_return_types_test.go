package auditor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/igor-php/igor-php/internal/analyzer"
	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// Without a framework map, a getter's return type is read through the project's Composer autoload,
// and a container dump saying the returned class is not shared clears the finding (#149).
func TestPlainScan_ReturnTypeOfADependency(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"composer.json": `{"autoload": {"psr-4": {"Min\\": "src/"}}, "autoload-dev": {"psr-4": {"Min\\Tests\\": ["tests/"]}}}`,
		"src/Thing.php": `<?php
namespace Min;
final class Thing { private string $n = ''; public function setName(string $n): void { $this->n = $n; } }`,
		"src/Provider.php": `<?php
namespace Min;
final readonly class Provider { public function get(): Thing { return new Thing(); } }`,
		"src/Ctrl.php": `<?php
namespace Min;
final class Ctrl {
    public function __construct(private Provider $provider) {}
    public function __invoke(): void { $thing = $this->provider->get(); $thing->setName('changed'); }
}`,
	}
	for name, code := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	audit := func(nonShared analyzer.NonSharedServiceMap) bool {
		aud := NewAuditor(config.Config{})
		aud.Root = root
		aud.NonSharedServices = nonShared
		if got := aud.GetMethodReturnType(`Min\Provider`, "get"); got != `Min\Thing` {
			t.Fatalf("Provider::get() must be read through composer.json, got %q", got)
		}
		findings, err := aud.Audit(filepath.Join(root, "src", "Ctrl.php"), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range findings {
			if f.Rule == symbol.RuleSingletonMutation {
				return true
			}
		}
		return false
	}

	if audit(analyzer.NonSharedServiceMap{`Min\Thing`: true}) {
		t.Error("the dump says Thing is not shared: the object get() returns is per call")
	}
	if !audit(nil) {
		t.Error("without a dump, igor cannot tell whether the returned object is shared: it keeps the finding")
	}
}

func TestComposerAutoload_FallsBackToComposerJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{
		"autoload": {"psr-4": {"App\\": "src/", "Lib\\": ["lib/", "legacy/"]}},
		"autoload-dev": {"psr-4": {"App\\Tests\\": "tests/"}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	autoload := loadComposerAutoload(root)
	for prefix, want := range map[string][]string{
		`App\`:       {filepath.Join(root, "src")},
		`Lib\`:       {filepath.Join(root, "lib"), filepath.Join(root, "legacy")},
		`App\Tests\`: {filepath.Join(root, "tests")},
	} {
		got := autoload.psr4[prefix]
		if len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", prefix, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: got %v, want %v", prefix, got, want)
			}
		}
	}
}
