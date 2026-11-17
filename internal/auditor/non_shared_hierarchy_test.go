package auditor

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// A trait or a parent class that only non-shared definitions use (a Laravel controller, a Livewire
// or Filament page component, rebuilt for every request) keeps no instance state across requests.
// Its static properties belong to the class and persist, so they are still reported. The same
// trait used by a shared service stays a leak.
func TestNonSharedHierarchy(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"SortableTrait.php": `<?php
namespace App\Traits;
trait SortableTrait {
    public string $sortField = 'id';
    private static array $seen = [];
    public function sortBy(string $field): void {
        $this->sortField = $field;
        static::$seen[] = $field;
    }
}`,
		"Page.php": `<?php
namespace Vendor\Ui;
abstract class Page {
    protected ?string $heading = null;
    public function mount(string $heading): void { $this->heading = $heading; }
}`,
		"CacheTrait.php": `<?php
namespace App\Traits;
trait CacheTrait {
    private array $cache = [];
    public function remember(string $key, mixed $value): void { $this->cache[$key] = $value; }
}`,
	}
	paths := map[string]string{}
	for name, code := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}

	aud := NewAuditor(config.Config{})
	aud.Symfony = &SymfonyBridge{
		Container: &symbol.SymfonyContainer{Definitions: map[string]symbol.SymfonyService{
			`controller.App\Livewire\UserSearch`: {Class: `App\Livewire\UserSearch`, Shared: false, Tags: map[string]any{symbol.EntrypointTag: []any{}}},
			`App\Services\Catalog`:               {Class: `App\Services\Catalog`, Shared: true},
		}},
		ClassToFile: map[string]string{
			`App\Traits\SortableTrait`: paths["SortableTrait.php"],
			`Vendor\Ui\Page`:           paths["Page.php"],
			`App\Traits\CacheTrait`:    paths["CacheTrait.php"],
		},
		// What find_class_files.php reports: the hierarchy of App\Services\Catalog only
		SharedHierarchy: map[string]bool{`App\Services\Catalog`: true, `App\Traits\CacheTrait`: true},
	}

	for class, want := range map[string]bool{
		`App\Traits\SortableTrait`: true,
		`Vendor\Ui\Page`:           true,
		`App\Traits\CacheTrait`:    false,
		`App\Services\Catalog`:     false, // a definition, not located through another one
		`App\Unknown\Helper`:       false, // not located at all: nothing is known about it
	} {
		if got := aud.Symfony.OnlyInNonSharedHierarchy(class); got != want {
			t.Errorf("OnlyInNonSharedHierarchy(%s) = %v, want %v", class, got, want)
		}
	}
	if !aud.IsExplicitlyNonShared(`App\Traits\SortableTrait`) {
		t.Error("a trait only used by a controller is non-shared")
	}
	if aud.IsExplicitlyNonShared(`App\Traits\CacheTrait`) {
		t.Error("a trait used by a shared service is not non-shared")
	}

	reported := func(file string) []string {
		findings, err := aud.Audit(paths[file], nil)
		if err != nil {
			t.Fatal(err)
		}
		var messages []string
		for _, f := range findings {
			messages = append(messages, f.Message)
		}
		return messages
	}

	sortable := reported("SortableTrait.php")
	if slices.Contains(sortable, "Mutation of state 'sortField' in SortableTrait::sortBy()") {
		t.Errorf("instance state of a trait used only by a controller must not be reported: %v", sortable)
	}
	if !slices.ContainsFunc(sortable, func(m string) bool { return m == "Mutation of state 'static::$seen' in SortableTrait::sortBy()" }) {
		t.Errorf("the static property of the same trait persists and must be reported: %v", sortable)
	}
	if page := reported("Page.php"); len(page) != 0 {
		t.Errorf("a parent class used only by controllers must not be reported: %v", page)
	}
	if cache := reported("CacheTrait.php"); !slices.Contains(cache, "Mutation of state 'cache' in CacheTrait::remember()") {
		t.Errorf("a trait of a shared service is still reported: %v", cache)
	}
}

// find_class_files.php reports the classes used by a shared definition, and the Go side reads it.
func TestFindClassFilesSharedHierarchy(t *testing.T) {
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("skipping: `php` not found in PATH")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	classes := `<?php
namespace App;
trait OnlyController { public $a; }
trait OnlyService { public $b; }
trait Both { public $c; }
abstract class BaseController { use Both; }
class SearchController extends BaseController { use OnlyController; }
class Catalog { use OnlyService, Both; }
class AliasedOnlyNonShared { use OnlyController; }
`
	if err := os.WriteFile(filepath.Join(root, "classes.php"), []byte(classes), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "autoload.php"), []byte("<?php require __DIR__.'/../classes.php';"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := &SymfonyBridge{Root: root}
	err := b.locateFilesViaReflection(`{"definitions": {
		"controller.App\\SearchController": {"class": "App\\SearchController", "shared": false},
		"App\\Catalog": {"class": "App\\Catalog", "shared": true},
		"aliased": {"class": "App\\AliasedOnlyNonShared", "shared": false}
	}, "aliases": {"App\\AliasedOnlyNonShared": "aliased"}}`)
	if err != nil {
		t.Fatal(err)
	}

	for _, class := range []string{`App\SearchController`, `App\BaseController`, `App\OnlyController`, `App\Catalog`, `App\OnlyService`, `App\Both`} {
		if b.ClassToFile[class] == "" {
			t.Errorf("%s should be located", class)
		}
	}
	for class, want := range map[string]bool{
		`App\Catalog`:              true,
		`App\OnlyService`:          true,
		`App\Both`:                 true, // also used by the shared Catalog
		`App\SearchController`:     false,
		`App\BaseController`:       false,
		`App\OnlyController`:       false,
		`App\AliasedOnlyNonShared`: false, // its alias points to a non-shared definition
	} {
		if b.SharedHierarchy[class] != want {
			t.Errorf("SharedHierarchy[%s] = %v, want %v", class, b.SharedHierarchy[class], want)
		}
	}
}
