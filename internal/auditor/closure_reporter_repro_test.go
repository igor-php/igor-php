package auditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// The reduced case of #139, as its reporter wrote it: the constructor only reads $map with
// count(), imported with "use function count;", as code styles that import native functions do.
// That import names the PHP function itself, so the closure is only read, and the finding goes.
// The same code with "use function Demo\count;" calls the project's own count(): igor cannot
// know what it does, and the finding stays a warning. When a file Composer always loads declares
// Demo\count(), an unqualified count() calls it, but "use function count;" still calls PHP's.
func TestReporterReproOfIssue139(t *testing.T) {
	for _, tc := range []struct {
		name, use string
		// declared is true when a file Composer always loads declares Demo\count()
		declared bool
		want     string // "" when the finding is dropped
	}{
		{"native count imported", "use function count;", false, ""},
		{"native count imported fully qualified", `use function \count;`, false, ""},
		{"project count imported", `use function Demo\count;`, false, "WARNING"},
		{"native count imported, Demo\\count declared", "use function count;", true, ""},
		{"no import, Demo\\count declared", "", true, "WARNING"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range []string{"vendor/composer", "src"} {
				if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			files := map[string]string{
				"vendor/composer/autoload_psr4.php": "<?php\n$vendorDir = dirname(__DIR__);\n$baseDir = dirname($vendorDir);\nreturn array(\n    'Demo\\\\' => array($baseDir . '/src'),\n);\n",
				"src/Mapper.php": `<?php

declare(strict_types=1);

namespace Demo;

final class Mapper
{
    /**
     * @param iterable<mixed> $source
     *
     * @return array<mixed>
     */
    public function map(string $class, iterable $source): array
    {
        return [$class, $source];
    }
}
`,
				"src/ArrayRewriter.php": `<?php

declare(strict_types=1);

namespace Demo;

` + tc.use + `

final class ArrayRewriter
{
    /**
     * @var array<mixed>
     */
    private array $source;

    /**
     * @param array<mixed> $source
     * @param array<mixed> $map
     */
    public function __construct(array $source, array $map)
    {
        // $map is only READ here. It is never stored, never iterated, never returned.
        $this->source = count($map) > 0 ? $source : [];
    }
}
`,
				"src/PriceClient.php": `<?php

declare(strict_types=1);

namespace Demo;

final class PriceClient
{
    public function __construct(
        private Mapper $mapper,
    ) {
    }

    /**
     * @return array<mixed>
     */
    public function latest(string $base): array
    {
        return $this->mapper->map(self::class, new ArrayRewriter(['rates' => []], [
            'rates' => static function (array $value) use ($base) {
                return [$base, $value];
            },
        ]));
    }
}
`,
			}
			if tc.declared {
				files["vendor/composer/autoload_files.php"] = "<?php\n$vendorDir = dirname(__DIR__);\n$baseDir = dirname($vendorDir);\nreturn array(\n    'a1b2' => $baseDir . '/src/functions.php',\n);\n"
				files["src/functions.php"] = "<?php\nnamespace Demo;\nfunction count(array $map): int { $GLOBALS['kept'][] = $map; return 1; }\n"
			}
			for name, code := range files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(code), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(root, "src", "PriceClient.php")
			aud := NewAuditor(config.Config{})
			aud.Symfony = &SymfonyBridge{Root: root,
				Container: &symbol.SymfonyContainer{Definitions: map[string]symbol.SymfonyService{
					`Demo\PriceClient`: {Class: `Demo\PriceClient`, Shared: true},
					`Demo\Mapper`:      {Class: `Demo\Mapper`, Shared: true},
				}},
				ClassToFile: map[string]string{
					`Demo\PriceClient`: path,
					`Demo\Mapper`:      filepath.Join(root, "src", "Mapper.php"),
				},
			}
			findings, err := aud.Audit(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			results := []symbol.AuditStatus{{FilePath: path, Findings: findings}}
			aud.ResolveClosureLeaks(results)
			if got := closureLeaks(results[0].Findings)["latest"]; got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// A receiver typed with an abstract class that does not declare the method, as foundry's
// Factory::afterInstantiate() (declared on ObjectFactory only): the warning says igor found no
// declaration on that class, not that it could not read it.
func TestUndeclaredMethodHint(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"vendor/composer", "src"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"vendor/composer/autoload_psr4.php": "<?php\n$vendorDir = dirname(__DIR__);\n$baseDir = dirname($vendorDir);\nreturn array(\n    'Demo\\\\' => array($baseDir . '/src'),\n);\n",
		"src/Factory.php":                   "<?php\nnamespace Demo;\nabstract class Factory {}\n",
		"src/ObjectFactory.php":             "<?php\nnamespace Demo;\nclass ObjectFactory extends Factory { private array $after = []; public function afterInstantiate(callable $c): static { $this->after[] = $c; return $this; } }\n",
		"src/Registry.php": `<?php
namespace Demo;
final class Registry
{
    public function __construct(private Factory $factory) {}

    public function get(string $name): Factory
    {
        return $this->factory->afterInstantiate(static function (object $o) use ($name) {
            return [$name, $o];
        });
    }
}
`,
	}
	for name, code := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "src", "Registry.php")
	aud := NewAuditor(config.Config{})
	aud.Symfony = &SymfonyBridge{Root: root,
		Container:   &symbol.SymfonyContainer{Definitions: map[string]symbol.SymfonyService{`Demo\Registry`: {Class: `Demo\Registry`, Shared: true}}},
		ClassToFile: map[string]string{`Demo\Registry`: path},
	}
	findings, err := aud.Audit(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := []symbol.AuditStatus{{FilePath: path, Findings: findings}}
	aud.ResolveClosureLeaks(results)
	for _, f := range results[0].Findings {
		if f.Rule == symbol.RuleClosureStateLeak {
			if f.Severity != "WARNING" || !strings.Contains(f.Remediation, `found no declaration of Demo\Factory::afterInstantiate()`) {
				t.Errorf("got %s %q", f.Severity, f.Remediation)
			}
			return
		}
	}
	t.Fatalf("the closure finding is gone: %+v", results[0].Findings)
}

// An interface that inherits the method from a parent interface declares it: the warning says
// igor could not read it (no body), not that it found no declaration.
func TestInheritedInterfaceMethodHint(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"vendor/composer", "src"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"vendor/composer/autoload_psr4.php": "<?php\n$vendorDir = dirname(__DIR__);\n$baseDir = dirname($vendorDir);\nreturn array(\n    'Demo\\\\' => array($baseDir . '/src'),\n);\n",
		"src/ParentVisitor.php":             "<?php\nnamespace Demo;\ninterface ParentVisitor { public function visit(callable $c): void; }\n",
		"src/ChildVisitor.php":              "<?php\nnamespace Demo;\ninterface ChildVisitor extends ParentVisitor {}\n",
		"src/Walker.php": `<?php
namespace Demo;
final class Walker
{
    public function __construct(private ChildVisitor $visitor) {}

    public function walk(string $name): void
    {
        $this->visitor->visit(static function () use ($name) {
            return $name;
        });
    }
}
`,
	}
	for name, code := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "src", "Walker.php")
	aud := NewAuditor(config.Config{})
	aud.Symfony = &SymfonyBridge{Root: root,
		Container:   &symbol.SymfonyContainer{Definitions: map[string]symbol.SymfonyService{`Demo\Walker`: {Class: `Demo\Walker`, Shared: true}}},
		ClassToFile: map[string]string{`Demo\Walker`: path},
	}
	findings, err := aud.Audit(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := []symbol.AuditStatus{{FilePath: path, Findings: findings}}
	aud.ResolveClosureLeaks(results)
	for _, f := range results[0].Findings {
		if f.Rule == symbol.RuleClosureStateLeak {
			if f.Severity != "WARNING" || !strings.Contains(f.Remediation, "could not read") {
				t.Errorf("got %s %q", f.Severity, f.Remediation)
			}
			return
		}
	}
	t.Fatalf("the closure finding is gone: %+v", results[0].Findings)
}
