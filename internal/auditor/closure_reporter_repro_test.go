package auditor

import (
	"os"
	"path/filepath"
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
