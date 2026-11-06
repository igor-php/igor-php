package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
namespace Symfony\Contracts\Service {
    interface ResetInterface { public function reset(); }
}
namespace App\Service {
    class BundleCollector implements \Symfony\Contracts\Service\ResetInterface { public function reset() {} }
    class TaggedCollector implements \Symfony\Contracts\Service\ResetInterface { public function reset() {} }
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

    // A bundle service implementing ResetInterface without kernel.reset is never reset
    $cb->definitions['app.bundle_collector'] = new \Symfony\Component\DependencyInjection\Definition('App\Service\BundleCollector');
    $taggedDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\TaggedCollector');
    $taggedDef->tags['kernel.reset'] = [['method' => 'reset']];
    $cb->definitions['app.tagged_collector'] = $taggedDef;

    // Private interface aliases exist before the removing passes...
    $resettableDef = new \Symfony\Component\DependencyInjection\Definition('App\Service\ResettableCache');
    $cb->definitions['App\Service\ResettableCache'] = $resettableDef;
    $cb->definitions['app.inlined_resettable'] = new \Symfony\Component\DependencyInjection\Definition('App\Service\InlinedResettable');
    $cb->aliases['App\Service\CacheInterface'] = 'App\Service\ResettableCache';
    $cb->aliases['App\Service\InlinedInterface'] = 'app.inlined_resettable';
    $cb->definitions['app.dead_service'] = new \Symfony\Component\DependencyInjection\Definition('App\Service\DeadService');
    $cb->aliases['App\Service\DeadInterface'] = 'app.dead_service';
    $cb->passes[0]['pass']->process($cb);

    // ...then RemovePrivateAliasesPass drops them, app.inlined_resettable gets inlined
    // into ParentService and app.dead_service is pruned as unused
    $cb->aliases = [];
    unset($cb->definitions['app.inlined_resettable'], $cb->definitions['app.dead_service']);
    $parentDef->arguments[] = new \Symfony\Component\DependencyInjection\Definition('App\Service\InlinedResettable');
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
    if (isset($data['aliases']['App\Service\DeadInterface'])) {
        fwrite(STDERR, "Alias to a pruned service must not be exported\n");
        exit(1);
    }
    if (($data['definitions']['app.bundle_collector']['resettable'] ?? null) !== false) {
        fwrite(STDERR, "A ResetInterface service without the kernel.reset tag must not be resettable\n");
        exit(1);
    }
    if (($data['definitions']['app.tagged_collector']['resettable'] ?? null) !== true) {
        fwrite(STDERR, "A service tagged kernel.reset must be resettable\n");
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

	// Report the test as skipped (not passed) when the demo's Composer dependencies are not installed,
	// or as failed when IGOR_LABS_REQUIRED is set. It compiles its own container: the lab's
	// cached service map is not needed.
	lab := leakLab{name: "demo-leak", prerequisites: requireLabDependencies}
	requireLab(t, lab, filepath.Join("..", "..", "examples", "demo-leak"))

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
// 6. Private interface alias to the dead service, removed along with it
$container->setAlias('IteratorAggregate', 'app.unused_dead_service')->setPublic(false);

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
if (isset($data['aliases']['IteratorAggregate'])) {
    fwrite(STDERR, "Alias to a pruned service must not be exported\n");
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

func TestServiceMapBuilder_ResettableOnlyWithOctaneListener(t *testing.T) {
	requirePHP(t)

	// The listener must live in a real file: the builder reads its source to find the services it resets
	dir := t.TempDir()
	listener := `<?php
namespace App\Listeners;

class ResetCollectors
{
    public function handle(): void
    {
        app(\App\Service\ListenedCollector::class)->reset();
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "ResetCollectors.php"), []byte(listener), 0o600); err != nil {
		t.Fatal(err)
	}

	phpScript := `
namespace Symfony\Contracts\Service {
    interface ResetInterface { public function reset(); }
}
namespace Illuminate\Contracts\Foundation {
    interface Application {}
}
namespace App\Service {
    // Implements ResetInterface, but nothing in Laravel calls it and no listener references it
    class LonelyCollector implements \Symfony\Contracts\Service\ResetInterface { public function reset() {} }
    // Reset by an Octane listener on a request event
    class ListenedCollector implements \Symfony\Contracts\Service\ResetInterface { public function reset() {} }
}
namespace {
    if ($argv[2] === 'octane') {
        eval('namespace Laravel\Octane; class Octane {}');
    }
    require $argv[1] . '/ResetCollectors.php';
    require 'src/laravel/ServiceMapBuilder.php';

    $app = new class implements \Illuminate\Contracts\Foundation\Application, \ArrayAccess {
        public function offsetExists($k): bool { return $k === 'config'; }
        public function offsetGet($k): mixed {
            return new class {
                public function get($key, $default) {
                    return $key === 'octane.listeners'
                        ? ['Laravel\\Octane\\Events\\RequestReceived' => ['App\\Listeners\\ResetCollectors']]
                        : $default;
                }
            };
        }
        public function offsetSet($k, $v): void {}
        public function offsetUnset($k): void {}
    };

    $builder = new \IgorPhp\Laravel\ServiceMapBuilder($app);
    $isResettable = \Closure::bind(fn (string $class) => $this->isResettable($class), $builder, \IgorPhp\Laravel\ServiceMapBuilder::class);

    if ($isResettable('App\\Service\\LonelyCollector') !== false) {
        fwrite(STDERR, "A ResetInterface service no Octane listener references must not be resettable\n");
        exit(1);
    }
    // Without Octane, the listener still in config/octane.php never runs
    $expected = $argv[2] === 'octane';
    if ($isResettable('App\\Service\\ListenedCollector') !== $expected) {
        fwrite(STDERR, "A service referenced by an Octane request listener must be resettable only when Octane runs\n");
        exit(1);
    }
    echo "SUCCESS\n";
}
`

	for _, model := range []string{"octane", "worker"} {
		cmd := exec.Command("php", "-r", phpScript, "--", dir, model)
		cmd.Dir = filepath.Join("..", "..")
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "SUCCESS") {
			t.Fatalf("ServiceMapBuilder resettable test (%s) failed: %v\nOutput: %s", model, err, string(output))
		}
	}
}

func TestServiceMapBuilder_PersistenceModelWithAndWithoutOctane(t *testing.T) {
	requirePHP(t)

	phpScript := `
namespace Illuminate\Contracts\Foundation {
    interface Application {}
}
namespace App\Service {
    class Booted {}
    class LazySingleton {}
    class ScopedService {}
    class Transient {}
    class Collector {}
}
namespace {
    if ($argv[1] === 'octane') {
        eval('namespace Laravel\Octane; class Octane {}');
    }
    require 'src/laravel/ServiceMapBuilder.php';

    $app = new class implements \Illuminate\Contracts\Foundation\Application, \ArrayAccess {
        // Read by the builder through a closure bound to the container, like Illuminate's protected properties
        protected array $instances = [];
        protected array $scopedInstances = ['App\\Service\\ScopedService'];
        protected array $aliases = [];

        public function __construct() { $this->instances['App\\Service\\Booted'] = new \App\Service\Booted(); }
        public function getBindings(): array {
            return [
                'App\\Service\\LazySingleton' => ['concrete' => 'App\\Service\\LazySingleton', 'shared' => true],
                'App\\Service\\ScopedService' => ['concrete' => 'App\\Service\\ScopedService', 'shared' => true],
                'App\\Service\\Transient' => ['concrete' => 'App\\Service\\Transient', 'shared' => false],
                // A factory singleton: only building it tells its class
                'collector' => ['concrete' => fn () => new \App\Service\Collector(), 'shared' => true],
            ];
        }
        public function bound($abstract): bool { return false; }
        public function getAlias($abstract) { return $abstract; }
        public function make($abstract) {
            $concrete = $this->getBindings()[$abstract]['concrete'] ?? $abstract;
            return $concrete instanceof \Closure ? $concrete() : new $concrete();
        }
        public function offsetExists($k): bool { return $k === 'config'; }
        public function offsetGet($k): mixed {
            return new class { public function get($key, $default) { return $default; } };
        }
        public function offsetSet($k, $v): void {}
        public function offsetUnset($k): void {}
    };

    $map = (new \IgorPhp\Laravel\ServiceMapBuilder($app))->build();
    $shared = array_map(fn (array $definition) => $definition['shared'], $map['definitions']);
    ksort($shared);
    echo json_encode($shared), "\n";
}
`

	run := func(model string) map[string]bool {
		t.Helper()
		cmd := exec.Command("php", "-r", phpScript, "--", model)
		cmd.Dir = filepath.Join("..", "..")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ServiceMapBuilder (%s) failed: %v\nOutput: %s", model, err, string(output))
		}
		var shared map[string]bool
		if err := json.Unmarshal(output, &shared); err != nil {
			t.Fatalf("ServiceMapBuilder (%s) printed no JSON: %v\nOutput: %s", model, err, string(output))
		}
		return shared
	}

	// Octane serves each request from a clone: only what the base application holds after boot persists
	octane := map[string]bool{
		"App\\Service\\Booted":        true,
		"App\\Service\\LazySingleton": false,
		"App\\Service\\ScopedService": false,
		"App\\Service\\Transient":     false,
	}
	// Without Octane the same application serves every request: singletons and scoped bindings persist,
	// including a factory singleton, built to learn its class
	worker := map[string]bool{
		"App\\Service\\Booted":        true,
		"App\\Service\\LazySingleton": true,
		"App\\Service\\ScopedService": true,
		"App\\Service\\Transient":     false,
		"collector":                   true,
	}

	for model, want := range map[string]map[string]bool{"octane": octane, "worker": worker} {
		if got := run(model); !reflect.DeepEqual(got, want) {
			t.Errorf("%s model: shared = %v, want %v", model, got, want)
		}
	}
}
