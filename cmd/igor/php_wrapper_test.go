package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		"../../src/php/DependencyInjection/Compiler/IgorAliasSnapshotPass.php",
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
        public const TYPE_BEFORE_REMOVING = 'beforeRemoving';
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
        public function hasDefinition(string $id): bool { return isset($this->definitions[$id]); }
        public function hasAlias(string $id): bool { return isset($this->aliases[$id]); }
        public function findDefinition(string $id): Definition {
            while (isset($this->aliases[$id])) { $id = $this->aliases[$id]; }
            if (!isset($this->definitions[$id])) { throw new \RuntimeException("Unknown service $id"); }
            return $this->definitions[$id];
        }
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
    require __DIR__ . '/../../src/php/DependencyInjection/Compiler/IgorAliasSnapshotPass.php';

    // 1. Verify bundle registers the alias snapshot before removing, and the discovery pass after removing
    $cb = new \Symfony\Component\DependencyInjection\ContainerBuilder();
    $bundle = new \IgorPhp\IgorBundle\IgorPhpBundle();
    $bundle->build($cb);

    if (count($cb->passes) !== 2) {
        fwrite(STDERR, "Expected 2 compiler passes, got " . count($cb->passes) . "\n");
        exit(1);
    }
    if (!$cb->passes[0]['pass'] instanceof \IgorPhp\IgorBundle\DependencyInjection\Compiler\IgorAliasSnapshotPass || $cb->passes[0]['type'] !== 'beforeRemoving') {
        fwrite(STDERR, "Expected IgorAliasSnapshotPass registered as 'beforeRemoving', got " . $cb->passes[0]['type'] . "\n");
        exit(1);
    }
    if (!$cb->passes[1]['pass'] instanceof \IgorPhp\IgorBundle\DependencyInjection\Compiler\IgorDiscoveryPass || $cb->passes[1]['type'] !== 'afterRemoving') {
        fwrite(STDERR, "Expected IgorDiscoveryPass registered as 'afterRemoving', got " . $cb->passes[1]['type'] . "\n");
        exit(1);
    }
    foreach ($cb->passes as $registered) {
        if ($registered['priority'] >= 0) {
            fwrite(STDERR, "Expected a negative priority so the pass runs last, got " . $registered['priority'] . "\n");
            exit(1);
        }
    }

    // 2. Verify process() captures inlined services (direct and wrapped) and skips excluded/synthetic
    $cacheDir = sys_get_temp_dir() . '/igor_test_' . uniqid();
    $cb->parameters['kernel.cache_dir'] = $cacheDir;

    $inlinedDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\InlinedHelper');
    $inlinedDef->shared = false; // inlined private service originally had shared false

    $wrappedInlinedDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\WrappedInlinedHelper');
    $wrappedInlinedDef->shared = false;
    $wrappedArg = new \Symfony\Component\DependencyInjection\Argument\ServiceClosureArgument($wrappedInlinedDef);

    $dualInlinedDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\DualLifecycleHelper');
    $dualInlinedDef->shared = false;

    $parentDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\ParentService', [$inlinedDef, $wrappedArg, $dualInlinedDef]);
    $parentDef->shared = true;

    $dualStandaloneDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\DualLifecycleHelper');
    $dualStandaloneDef->shared = false;

    $excludedDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\ExcludedService');
    $excludedDef->tags['container.excluded'] = [[]];

    $syntheticDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\SyntheticService');
    $syntheticDef->synthetic = true;

    $cb->definitions['App\Service\ParentService'] = $parentDef;
    $cb->definitions['App\Service\DualLifecycleHelper'] = $dualStandaloneDef;
    $cb->definitions['App\Service\ExcludedService'] = $excludedDef;
    $cb->definitions['App\Service\SyntheticService'] = $syntheticDef;

    // Private interface aliases exist before the removing passes...
    $resettableDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\ResettableCache');
    $cb->definitions['App\Service\ResettableCache'] = $resettableDef;
    $cb->definitions['app.inlined_resettable'] = new \Symfony\Component\DependencyInjection\Definition('App\Service\InlinedResettable');
    $cb->aliases['App\Service\CacheInterface'] = 'App\Service\ResettableCache';
    $cb->aliases['App\Service\InlinedInterface'] = 'app.inlined_resettable';
    $cb->passes[0]['pass']->process($cb);

    // ...then RemovePrivateAliasesPass drops them and app.inlined_resettable gets inlined
    $cb->aliases = [];
    unset($cb->definitions['app.inlined_resettable']);
    $cb->passes[1]['pass']->process($cb);

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
    if (($data['aliases']['App\Service\CacheInterface'] ?? null) !== 'App\Service\ResettableCache') {
        fwrite(STDERR, "Private interface alias removed before the discovery pass must be restored from the snapshot\n");
        exit(1);
    }
    if (($data['aliases']['App\Service\InlinedInterface'] ?? null) !== 'App\Service\InlinedResettable') {
        fwrite(STDERR, "Alias to an inlined service must point to the service class\n");
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
            // Since it was wrapped in a ServiceClosureArgument with shared=false, it must retain shared=false
            if ($def['shared'] !== false) {
                fwrite(STDERR, "Wrapped inlined service in ServiceClosureArgument with shared=false must retain shared=false\n");
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

    // Verify each definition preserves its own lifecycle intact:
    // the prototype definition keeps shared=false, and the inlined definition in shared parent keeps shared=true
    $dualDefinitions = [];
    foreach ($data['definitions'] as $id => $def) {
        if ($def['class'] === 'App\Service\DualLifecycleHelper') {
            $dualDefinitions[$id] = $def;
        }
    }
    if (count($dualDefinitions) !== 2) {
        fwrite(STDERR, "Expected 2 definitions for DualLifecycleHelper, got " . count($dualDefinitions) . "\n");
        exit(1);
    }
    if ($dualDefinitions['App\Service\DualLifecycleHelper']['shared'] !== false) {
        fwrite(STDERR, "Standalone prototype definition should preserve shared=false\n");
        exit(1);
    }
    $inlinedFound = false;
    foreach ($dualDefinitions as $id => $def) {
        if (str_starts_with($id, 'inlined.')) {
            $inlinedFound = true;
            if ($def['shared'] !== true) {
                fwrite(STDERR, "Inlined definition in shared parent must have shared=true\n");
                exit(1);
            }
        }
    }
    if (!$inlinedFound) {
        fwrite(STDERR, "Inlined definition for DualLifecycleHelper not found\n");
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

func TestIgorDiscoveryPass_RealSymfonyCompilation(t *testing.T) {
	requirePHP(t)

	// Report the test as skipped (not passed) when the demo's Composer dependencies are not installed
	autoload := filepath.Join("..", "..", "examples", "demo-leak", "vendor", "autoload.php")
	if _, err := os.Stat(autoload); err != nil {
		t.Skip("skipping: examples/demo-leak dependencies not installed (run `composer install -d examples/demo-leak`)")
	}
	if err := exec.Command("php", "-r", "exit(PHP_VERSION_ID >= 80400 ? 0 : 1);").Run(); err != nil {
		t.Skip("skipping: examples/demo-leak requires PHP >= 8.4 (Symfony 8)")
	}

	phpScript := `
require __DIR__ . '/../../examples/demo-leak/vendor/autoload.php';
require __DIR__ . '/../../src/php/IgorPhpBundle.php';
require __DIR__ . '/../../src/php/DependencyInjection/Compiler/IgorDiscoveryPass.php';
require __DIR__ . '/../../src/php/DependencyInjection/Compiler/IgorAliasSnapshotPass.php';

use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\DependencyInjection\Definition;
use Symfony\Component\DependencyInjection\Reference;
use Symfony\Component\DependencyInjection\Argument\ServiceClosureArgument;
use IgorPhp\IgorBundle\IgorPhpBundle;

$container = new ContainerBuilder();
$cacheDir = sys_get_temp_dir() . '/igor_real_test_' . uniqid();
$container->setParameter('kernel.cache_dir', $cacheDir);

// 1. Shared parent service
$parent = new Definition('stdClass');
$parent->setPublic(true);
$parent->setShared(true);

// 2. Private service inlined into parent by Symfony
$inlined = new Definition('ArrayObject');
$inlined->setPublic(false);
$inlined->setShared(false);
$container->setDefinition('app.inlined_helper', $inlined);

// 3. Service wrapped in ServiceClosureArgument with shared=false
$closureService = new Definition('ArrayIterator');
$closureService->setPublic(false);
$closureService->setShared(false);
$container->setDefinition('app.closure_helper', $closureService);
$wrappedArg = new ServiceClosureArgument(new Reference('app.closure_helper'));

$parent->setArguments([new Reference('app.inlined_helper'), $wrappedArg]);
$container->setDefinition('app.parent', $parent);

// 5. Private interface alias (as autowiring registers them), removed by RemovePrivateAliasesPass
$container->setAlias('Countable', 'app.inlined_helper')->setPublic(false);

// 4. Dead/unreferenced private service that Symfony will prune
$unused = new Definition('SplStack');
$unused->setPublic(false);
$container->setDefinition('app.unused_dead_service', $unused);

// Register bundle
$bundle = new IgorPhpBundle();
$bundle->build($container);

// Compile container executing real Symfony passes (InlineServiceDefinitionsPass, RemoveUnusedDefinitionsPass)
$container->compile();

$mapFile = $cacheDir . '/igor_service_map.json';
if (!file_exists($mapFile)) {
    fwrite(STDERR, "Service map was not created at $mapFile\n");
    exit(1);
}
$data = json_decode(file_get_contents($mapFile), true);

if (isset($data['definitions']['app.unused_dead_service'])) {
    fwrite(STDERR, "Unused service should have been pruned by RemoveUnusedDefinitionsPass\n");
    exit(1);
}
if (!isset($data['definitions']['app.parent'])) {
    fwrite(STDERR, "Parent service must be in service map\n");
    exit(1);
}
if ($container->hasAlias('Countable')) {
    fwrite(STDERR, "Private alias should have been removed by RemovePrivateAliasesPass\n");
    exit(1);
}
if (($data['aliases']['Countable'] ?? null) !== 'ArrayObject') {
    fwrite(STDERR, "Private interface alias must be kept in the service map and point to the inlined class\n");
    exit(1);
}

$foundInlined = false;
$foundClosureInlined = false;
foreach ($data['definitions'] as $id => $def) {
    if ($def['class'] === 'ArrayObject') {
        $foundInlined = true;
        if ($def['shared'] !== true) {
            fwrite(STDERR, "Inlined ArrayObject in shared parent must have shared=true\n");
            exit(1);
        }
    }
    if ($def['class'] === 'ArrayIterator') {
        $foundClosureInlined = true;
        if ($def['shared'] !== false) {
            fwrite(STDERR, "Inlined ArrayIterator in ServiceClosureArgument must retain shared=false\n");
            exit(1);
        }
    }
}

if (!$foundInlined) {
    fwrite(STDERR, "Inlined ArrayObject was not found in service map\n");
    exit(1);
}
if (!$foundClosureInlined) {
    fwrite(STDERR, "Inlined ArrayIterator was not found in service map\n");
    exit(1);
}

// Clean up
@unlink($mapFile);
@rmdir($cacheDir);
echo "SUCCESS\n";
`

	cmd := exec.Command("php", "-r", phpScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Real Symfony container compilation test failed: %v\nOutput: %s", err, string(output))
	}
	if !strings.Contains(string(output), "SUCCESS") {
		t.Fatalf("Real Symfony container compilation test did not complete: %s", string(output))
	}
}
