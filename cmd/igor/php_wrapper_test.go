package main

import (
	"os/exec"
	"testing"
)

// requirePHP skips the test when no `php` binary is on PATH. Igor's PHP-dependent
// paths (Symfony container introspection, reflection-based file location, `php -l`
// syntax checks) can only run where PHP is installed — e.g. inside a container
// with PHP — so on a PHP-less host these tests skip rather than fail.
func requirePHP(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("skipping: `php` not found in PATH (run PHP-dependent tests in a PHP-enabled environment)")
	}
}

func TestPhpWrapperSyntax(t *testing.T) {
	requirePHP(t)
	files := []string{
		"../../internal/auditor/find_class_files.php",
		"../../src/php/IgorPhpBundle.php",
		"../../src/php/DependencyInjection/Compiler/IgorDiscoveryPass.php",
	}
	for _, f := range files {
		cmd := exec.Command("php", "-l", f)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("PHP syntax check failed for %s:\n%s", f, string(output))
		}
	}
}

func TestIgorDiscoveryPass_AfterRemovingAndInlinedDefinitions(t *testing.T) {
	requirePHP(t)

	phpScript := `
namespace Symfony\Component\DependencyInjection\Compiler {
    interface CompilerPassInterface {
        public function process(\Symfony\Component\DependencyInjection\ContainerBuilder $container): void;
    }
    class PassConfig {
        public const TYPE_AFTER_REMOVING = 'afterRemoving';
    }
}
namespace Symfony\Component\DependencyInjection {
    class ContainerBuilder {
        public array $passes = [];
        public array $definitions = [];
        public array $aliases = [];
        public array $parameters = [];
        public function addCompilerPass($pass, string $type = 'beforeOptimization', int $priority = 0): static {
            $this->passes[] = ['pass' => $pass, 'type' => $type, 'priority' => $priority];
            return $this;
        }
        public function getDefinitions(): array { return $this->definitions; }
        public function getAliases(): array { return $this->aliases; }
        public function getParameter(string $name): mixed { return $this->parameters[$name] ?? null; }
        public function getParameterBag(): object {
            return new class {
                public function resolveValue($v) { return $v; }
            };
        }
    }
    class Definition {
        public ?string $class = null;
        public bool $shared = true;
        public bool $public = true;
        public bool $synthetic = false;
        public array $tags = [];
        public array $arguments = [];
        public array $methodCalls = [];
        public array $properties = [];
        public mixed $configurator = null;
        public mixed $factory = null;

        public function __construct(?string $class = null, array $arguments = []) {
            $this->class = $class;
            $this->arguments = $arguments;
        }
        public function getClass(): ?string { return $this->class; }
        public function isShared(): bool { return $this->shared; }
        public function isPublic(): bool { return $this->public; }
        public function isSynthetic(): bool { return $this->synthetic; }
        public function hasTag(string $name): bool { return isset($this->tags[$name]); }
        public function getArguments(): array { return $this->arguments; }
        public function getMethodCalls(): array { return $this->methodCalls; }
        public function getProperties(): array { return $this->properties; }
        public function getConfigurator(): mixed { return $this->configurator; }
        public function getFactory(): mixed { return $this->factory; }
    }
    class Reference {
        public function __construct(private string $id) {}
        public function __toString(): string { return $this->id; }
    }
}
namespace Symfony\Component\DependencyInjection\Argument {
    class ServiceClosureArgument {
        public function __construct(private mixed $value) {}
        public function getValues(): array { return [$this->value]; }
    }
}
namespace Symfony\Component\HttpKernel\Bundle {
    abstract class Bundle {
        public function build(\Symfony\Component\DependencyInjection\ContainerBuilder $container): void {}
    }
}
namespace {
    require __DIR__ . '/../../src/php/IgorPhpBundle.php';
    require __DIR__ . '/../../src/php/DependencyInjection/Compiler/IgorDiscoveryPass.php';

    // 1. Verify bundle registers with PassConfig::TYPE_AFTER_REMOVING
    $cb = new \Symfony\Component\DependencyInjection\ContainerBuilder();
    $bundle = new \IgorPhp\IgorBundle\IgorPhpBundle();
    $bundle->build($cb);

    if (count($cb->passes) !== 1) {
        fwrite(STDERR, "Expected 1 compiler pass, got " . count($cb->passes) . "\n");
        exit(1);
    }
    if ($cb->passes[0]['type'] !== 'afterRemoving') {
        fwrite(STDERR, "Expected pass type 'afterRemoving', got " . $cb->passes[0]['type'] . "\n");
        exit(1);
    }

    // 2. Verify process() captures inlined services (direct and wrapped) and skips excluded/synthetic
    $cacheDir = sys_get_temp_dir() . '/igor_test_' . uniqid();
    $cb->parameters['kernel.cache_dir'] = $cacheDir;

    $inlinedDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\InlinedHelper');
    $inlinedDef->shared = false; // inlined private service originally had shared false

    $wrappedInlinedDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\WrappedInlinedHelper');
    $wrappedInlinedDef->shared = false;
    $wrappedArg = new \Symfony\Component\DependencyInjection\Argument\ServiceClosureArgument($wrappedInlinedDef);

    $parentDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\ParentService', [$inlinedDef, $wrappedArg]);
    $parentDef->shared = true;

    $excludedDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\ExcludedService');
    $excludedDef->tags['container.excluded'] = [[]];

    $syntheticDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\SyntheticService');
    $syntheticDef->synthetic = true;

    $cb->definitions['App\Service\ParentService'] = $parentDef;
    $cb->definitions['App\Service\ExcludedService'] = $excludedDef;
    $cb->definitions['App\Service\SyntheticService'] = $syntheticDef;

    $pass = new \IgorPhp\IgorBundle\DependencyInjection\Compiler\IgorDiscoveryPass();
    $pass->process($cb);

    $mapFile = $cacheDir . '/igor_service_map.json';
    if (!file_exists($mapFile)) {
        fwrite(STDERR, "Map file was not created at $mapFile\n");
        exit(1);
    }
    $data = json_decode(file_get_contents($mapFile), true);

    if (!isset($data['definitions']['App\Service\ParentService'])) {
        fwrite(STDERR, "ParentService was not found in service map\n");
        exit(1);
    }
    if (isset($data['definitions']['App\Service\ExcludedService'])) {
        fwrite(STDERR, "ExcludedService should not be present in service map\n");
        exit(1);
    }
    if (isset($data['definitions']['App\Service\SyntheticService'])) {
        fwrite(STDERR, "SyntheticService should not be present in service map\n");
        exit(1);
    }

    // Check direct inlined definition presence
    $foundInlined = false;
    $foundWrappedInlined = false;
    foreach ($data['definitions'] as $id => $def) {
        if ($def['class'] === 'App\Service\InlinedHelper') {
            $foundInlined = true;
            if ($def['shared'] !== true) {
                fwrite(STDERR, "Inlined service in shared parent should inherit shared=true\n");
                exit(1);
            }
            if (!str_starts_with($id, 'inlined.App\Service\InlinedHelper.')) {
                fwrite(STDERR, "Inlined id prefix invalid: $id\n");
                exit(1);
            }
        }
        if ($def['class'] === 'App\Service\WrappedInlinedHelper') {
            $foundWrappedInlined = true;
            if ($def['shared'] !== true) {
                fwrite(STDERR, "Wrapped inlined service in shared parent should inherit shared=true\n");
                exit(1);
            }
            if (!str_starts_with($id, 'inlined.App\Service\WrappedInlinedHelper.')) {
                fwrite(STDERR, "Wrapped inlined id prefix invalid: $id\n");
                exit(1);
            }
        }
    }
    if (!$foundInlined) {
        fwrite(STDERR, "Inlined definition was not found in service map\n");
        exit(1);
    }
    if (!$foundWrappedInlined) {
        fwrite(STDERR, "Wrapped inlined definition (ServiceClosureArgument) was not found in service map\n");
        exit(1);
    }

    // Clean up
    @unlink($mapFile);
    @rmdir($cacheDir);
    echo "SUCCESS\n";
}
`

	cmd := exec.Command("php", "-r", phpScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("IgorDiscoveryPass test failed: %v\nOutput: %s", err, string(output))
	}
}
