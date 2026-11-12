package auditor

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// fakeArtisan writes an artisan script that answers `igor:dump` like the Laravel provider does,
// plus an autoloader so the reflection helper can locate the mapped classes.
func fakeArtisan(t *testing.T, root, dumpBody string) {
	t.Helper()
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("php is not available")
	}
	files := map[string]string{
		"artisan":        "<?php\n" + dumpBody,
		"src/Mailer.php": "<?php\nnamespace App;\nclass Mailer {}\n",
		"src/Uri.php":    "<?php\nnamespace App;\nclass Uri {}\n",
		"vendor/autoload.php": "<?php\nrequire_once __DIR__ . '/../src/Mailer.php';\n" +
			"require_once __DIR__ . '/../src/Uri.php';\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

const laravelServiceMap = `{
	"definitions": {
		"App\\Mailer": {"class": "App\\Mailer", "public": true, "shared": true, "resettable": false, "arguments": [], "tags": []},
		"App\\Uri": {"class": "App\\Uri", "public": true, "shared": false, "resettable": false, "arguments": [], "tags": []}
	},
	"aliases": {"mailer": "App\\Mailer"},
	"entrypoint_files": ["routes/web.php"],
	"warnings": ["Could not warm cache: connection refused"]
}`

func TestDetectLaravel_NotLaravel(t *testing.T) {
	b, err := DetectLaravel(t.TempDir(), config.Config{})
	if err != nil || b != nil {
		t.Fatalf("expected nil, nil without artisan, got %v, %v", b, err)
	}
}

func TestDetectLaravel_LoadsServiceMap(t *testing.T) {
	root := t.TempDir()
	// The map is written where igor asks for it: the app may use a custom bootstrap directory
	fakeArtisan(t, root, `if (($argv[1] ?? '') !== 'igor:dump' || !str_starts_with($argv[2] ?? '', '--path=')) { exit(1); }
$path = substr($argv[2], strlen('--path='));
@mkdir(dirname($path), 0777, true);
file_put_contents($path, <<<'JSON'
`+laravelServiceMap+`
JSON);
`)

	b, err := DetectLaravel(root, config.Config{})
	if err != nil {
		t.Fatalf("DetectLaravel failed: %v", err)
	}
	if b == nil || b.Framework != FrameworkLaravel {
		t.Fatalf("expected a Laravel bridge, got %+v", b)
	}
	if !b.IsSharedService("App\\Mailer") {
		t.Error("App\\Mailer persists in the worker and must be audited")
	}
	if b.IsSharedService("App\\Uri") {
		t.Error("App\\Uri is built per request and must not be audited as shared")
	}
	if got := b.Container.EntrypointFiles; len(got) != 1 || got[0] != "routes/web.php" {
		t.Errorf("expected the closure route files to be loaded, got %v", got)
	}
	if got := b.Container.Warnings; len(got) != 1 {
		t.Errorf("expected the exporter warnings to be loaded, got %v", got)
	}
	if got := b.ClassToFile["App\\Mailer"]; !strings.HasSuffix(filepath.ToSlash(got), "src/Mailer.php") {
		t.Errorf("expected App\\Mailer to be located in src/Mailer.php, got %q", got)
	}

	aud := NewAuditor(config.DefaultConfig())
	aud.UseFramework(b)
	if !aud.IsSafeNamespace("Illuminate\\Http\\Request") || !aud.IsSafeNamespace("Laravel\\Octane\\Octane") {
		t.Error("the Laravel framework namespaces must be safe when Laravel is detected")
	}
	if !aud.IsSafeNamespace("Symfony\\Component\\HttpFoundation\\Request") {
		t.Error("the default safe namespaces must be kept")
	}
}

func TestDetectLaravel_DumpFails(t *testing.T) {
	root := t.TempDir()
	fakeArtisan(t, root, "fwrite(STDERR, 'Command \"igor:dump\" is not defined.');\nexit(1);\n")

	_, err := DetectLaravel(root, config.Config{})
	if err == nil {
		t.Fatal("expected an error when igor:dump fails")
	}
	if !strings.Contains(err.Error(), "IgorServiceProvider") || !strings.Contains(err.Error(), "is not defined") {
		t.Errorf("the error must explain the failure and how to fix it, got: %v", err)
	}
}

// The worker model written by igor:dump reaches the stale dependency rule: without Octane, the
// container and the config are shared by every request, the request is not (#121).
func TestAudit_LaravelPersistenceModel(t *testing.T) {
	var container symbol.SymfonyContainer
	if err := json.Unmarshal([]byte(`{"persistence": "worker", "definitions": {}}`), &container); err != nil {
		t.Fatal(err)
	}
	if container.Persistence != symbol.PersistenceWorker {
		t.Fatalf("the map's persistence field must be read, got %q", container.Persistence)
	}

	path := filepath.Join(t.TempDir(), "ReportService.php")
	if err := os.WriteFile(path, []byte(`<?php
namespace App\Services;
use Illuminate\Contracts\Foundation\Application;
use Illuminate\Http\Request;
final class ReportService {
    public function __construct(private Application $app, private Request $request) {}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := func(persistence string) []string {
		aud := NewAuditor(config.Config{})
		aud.UseFramework(&SymfonyBridge{Framework: FrameworkLaravel, Container: &symbol.SymfonyContainer{Persistence: persistence}})
		findings, err := aud.Audit(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		var props []string
		for _, f := range findings {
			if f.Rule == symbol.RuleStaleDependency {
				props = append(props, f.Message)
			}
		}
		return props
	}
	if got := stale(symbol.PersistenceOctane); len(got) != 2 {
		t.Errorf("under Octane, the container and the request are stale: got %q", got)
	}
	if got := stale(symbol.PersistenceWorker); len(got) != 1 || !strings.Contains(got[0], "'request'") {
		t.Errorf("without Octane, only the request is stale: got %q", got)
	}
}
