package auditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// closureLeaks gives the severity of the closure state leak finding of each method.
func closureLeaks(findings []symbol.Finding) map[string]string {
	out := map[string]string{}
	for _, f := range findings {
		if f.Rule == symbol.RuleClosureStateLeak {
			out[f.ContextMethod] = f.Severity
		}
	}
	return out
}

func TestResolveClosureLeaks(t *testing.T) {
	aud := NewAuditor(config.Config{})
	path := filepath.Join("..", "..", "test", "fixtures", "closure_retention.php")

	findings, err := aud.Audit(path, nil)
	if err != nil {
		t.Fatalf("failed to audit fixture: %v", err)
	}
	before := closureLeaks(findings)

	results := []symbol.AuditStatus{{FilePath: path, Findings: findings}}
	if !aud.ResolveClosureLeaks(results) {
		t.Fatal("expected some closure findings to be dropped or downgraded")
	}
	after := closureLeaks(results[0].Findings)

	cases := []struct {
		method string
		want   string // "" when the finding is dropped
		why    string
	}{
		{"storedDirectly", "ERROR", "addListener() appends the closure to a property"},
		{"storedThroughPrivateMethod", "ERROR", "keepLater() hands it to a private method that stores it"},
		{"storedInsideAnArray", "ERROR", "the array holding the closure is stored"},
		{"storedByATrait", "ERROR", "defer() comes from a trait that stores it"},

		{"calledOnly", "", "wrap() calls the closure and returns"},
		{"calledThroughAnotherMethod", "", "retry() hands it to wrap(), which only calls it"},
		{"calledThroughAStaticHelper", "", "viaHelper() hands it to a static method that only calls it"},
		{"calledForEachListener", "", "visit() calls it in a loop"},
		{"calledByATrait", "", "immediately() comes from a trait that only calls it"},
		{"knownTransaction", "", "wrapInTransaction() is a known synchronous callback"},
		{"knownCacheCallback", "", "CacheInterface::get() is a known synchronous callback"},

		{"storedWithCoalesceAssign", "ERROR", "once() stores it with ??="},
		{"storedWithPlusAssign", "ERROR", "merge() stores it with +="},
		{"storedThroughAnotherParameter", "ERROR", "viaOptions() reuses a parameter to hold it, then stores that"},
		{"storedThroughACopyOfAProperty", "ERROR", "viaCopy() appends to a copy of a property, then stores the copy back"},
		{"registeredByAConstructor", "ERROR", "spawn() creates an object whose constructor registers a shutdown function"},
		{"boxedAndReturned", "", "box() creates an object that holds it and returns that object"},
		{"passedToAShadowingFunction", "WARNING", "App\\Closure\\array_map() is not PHP's array_map()"},
		{"calledOnAdaptedTraits", "WARNING", "insteadof picks the trait: the method is not read"},
		{"storedThroughParentInATrait", "ERROR", "parent:: in a trait is the parent of the class using it"},
		{"storedThroughSelfInATrait", "ERROR", "self:: in a trait is the class using it"},

		{"registeredThroughAnInterface", "ERROR", "an interface cannot be read, and addListener() registers"},
		{"visitedThroughAnInterface", "WARNING", "an interface cannot be read, and visit() says nothing"},
		{"calledOnAnAbstractClass", "WARNING", "an abstract class may be any subclass"},
		{"calledOnAClassWithASubclass", "WARNING", "a subclass overrides run() and stores the closure"},
		{"registeredOnAnUntypedProperty", "ERROR", "no type to read, and addListener() registers"},
		{"calledOnAnUntypedProperty", "WARNING", "no type to read, and process() says nothing"},
		{"calledWithANamedArgument", "WARNING", "a named argument is not matched to its parameter"},
	}
	for _, c := range cases {
		if before[c.method] != "ERROR" {
			t.Errorf("%s: expected the visitor to report an ERROR before the auditor reads the method, got %q", c.method, before[c.method])
		}
		if after[c.method] != c.want {
			t.Errorf("%s: got %q, want %q (%s)", c.method, after[c.method], c.want, c.why)
		}
	}
	if len(before) != len(cases) {
		t.Errorf("the fixture has %d closure findings, the test covers %d", len(before), len(cases))
	}
	for _, f := range results[0].Findings {
		if f.Rule == symbol.RuleClosureStateLeak && f.Severity == "WARNING" && !strings.Contains(f.Remediation, "could not read") {
			t.Errorf("%s: a downgraded finding must say igor could not read the method: %q", f.ContextMethod, f.Remediation)
		}
	}
}

// With a container, the type of the property resolves to the class of the service behind it:
// an interface, a vendor class in a safe namespace, a decorator.
func TestResolveClosureLeaks_ThroughTheContainer(t *testing.T) {
	dir := t.TempDir()
	write := func(name, code string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	dispatcher := write("EventDispatcher.php", `<?php
namespace Symfony\Component\EventDispatcher;
class EventDispatcher implements EventDispatcherInterface {
    private array $listeners = [];
    public function addListener(string $eventName, callable|array $listener, int $priority = 0): void {
        $this->listeners[$eventName][$priority][] = $listener;
    }
}`)
	traceable := write("TraceableEventDispatcher.php", `<?php
namespace Symfony\Component\EventDispatcher\Debug;
use Symfony\Component\EventDispatcher\EventDispatcherInterface;
class TraceableEventDispatcher implements EventDispatcherInterface {
    public function __construct(private EventDispatcherInterface $dispatcher) {}
    public function addListener(string $eventName, callable|array $listener, int $priority = 0): void {
        $this->dispatcher->addListener($eventName, $listener, $priority);
    }
}`)
	manager := write("EntityManager.php", `<?php
namespace Doctrine\ORM;
class EntityManager implements EntityManagerInterface {
    public function execute(callable $func): mixed {
        $this->conn->beginTransaction();
        try {
            $return = $func($this);
            $this->conn->commit();
            return $return;
        } finally {
            $this->close();
        }
    }
}`)
	app := write("Consumer.php", `<?php
namespace App;
use Doctrine\ORM\EntityManagerInterface;
use Symfony\Component\EventDispatcher\EventDispatcherInterface;
class Consumer {
    public function __construct(
        private EventDispatcherInterface $dispatcher,
        private EntityManagerInterface $entityManager,
    ) {}
    public function listen(string $optin): void {
        $this->dispatcher->addListener('kernel.response', function () use ($optin) { return $optin; });
    }
    public function execute(string $optin): void {
        $this->entityManager->execute(function () use ($optin) { return $optin; });
    }
}`)

	aud := NewAuditor(config.Config{SafeNamespaces: []string{"Symfony\\", "Doctrine\\"}})
	aud.Symfony = &SymfonyBridge{
		Container: &symbol.SymfonyContainer{
			Definitions: map[string]symbol.SymfonyService{
				"event_dispatcher":                    {Class: `Symfony\Component\EventDispatcher\Debug\TraceableEventDispatcher`, Shared: true, Decorates: "debug.event_dispatcher.inner"},
				"debug.event_dispatcher.inner":        {Class: `Symfony\Component\EventDispatcher\EventDispatcher`, Shared: true},
				"doctrine.orm.default_entity_manager": {Class: `Doctrine\ORM\EntityManager`, Shared: true},
				`App\Consumer`:                        {Class: `App\Consumer`, Shared: true},
			},
			Aliases: map[string]interface{}{
				`Symfony\Component\EventDispatcher\EventDispatcherInterface`: "event_dispatcher",
				`Doctrine\ORM\EntityManagerInterface`:                        "doctrine.orm.default_entity_manager",
			},
		},
		ClassToFile: map[string]string{
			`Symfony\Component\EventDispatcher\EventDispatcher`:                dispatcher,
			`Symfony\Component\EventDispatcher\Debug\TraceableEventDispatcher`: traceable,
			`Doctrine\ORM\EntityManager`:                                       manager,
			`App\Consumer`:                                                     app,
		},
	}

	findings, err := aud.Audit(app, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := []symbol.AuditStatus{{FilePath: app, Findings: findings}}
	aud.ResolveClosureLeaks(results)
	after := closureLeaks(results[0].Findings)

	if after["listen"] != "ERROR" {
		t.Errorf("listen: the decorator hands the closure to the dispatcher it decorates, which stores it: got %q", after["listen"])
	}
	// execute() is neither a registration name nor a known callback: only reading it drops the finding
	if _, reported := after["execute"]; reported {
		t.Errorf("execute: EntityManager::execute() only calls the closure, got %q", after["execute"])
	}

	// Reading the dispatcher is what keeps the error, not its name: a decorator that swallows
	// the listener leaves nothing to leak
	if err := os.WriteFile(traceable, []byte(`<?php
namespace Symfony\Component\EventDispatcher\Debug;
class TraceableEventDispatcher {
    public function addListener(string $eventName, callable|array $listener, int $priority = 0): void {
    }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, _ = aud.Audit(app, nil)
	results = []symbol.AuditStatus{{FilePath: app, Findings: findings}}
	aud.ResolveClosureLeaks(results)
	if _, reported := closureLeaks(results[0].Findings)["listen"]; reported {
		t.Errorf("listen: a method that ignores the closure cannot leak it")
	}
}

// The services injected into the caller decide which implementation a property holds, not the
// default service of its type.
func TestResolveClosureLeaks_InjectedImplementation(t *testing.T) {
	dir := t.TempDir()
	write := func(name, code string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	syncBus := write("SyncBus.php", `<?php
namespace Lib\Bus;
final class SyncBus implements BusInterface, \Countable {
    public function run(callable $work): mixed { return $work(); }
}`)
	queueBus := write("QueueBus.php", `<?php
namespace Lib\Bus;
final class QueueBus implements BusInterface {
    private array $queue = [];
    public function run(callable $work): mixed { $this->queue[] = $work; return null; }
}`)
	locator := write("Locator.php", `<?php
namespace Lib\Bus;
final class Locator implements \Countable, \IteratorAggregate {}`)
	consumer := func(class string) string {
		return write(class+".php", `<?php
namespace App;
use Lib\Bus\BusInterface;
class `+class+` {
    public function __construct(private BusInterface $bus) {}
    public function handle(string $optin): void {
        $this->bus->run(function () use ($optin) { return $optin; });
    }
}`)
	}
	byDefault, explicit, both, unlisted := consumer("ByDefault"), consumer("Explicit"), consumer("Both"), consumer("Unlisted")

	service := func(id string) map[string]any { return map[string]any{"type": "service", "id": id} }
	aud := NewAuditor(config.Config{})
	aud.Symfony = &SymfonyBridge{
		Container: &symbol.SymfonyContainer{
			Definitions: map[string]symbol.SymfonyService{
				"bus.sync":      {Class: `Lib\Bus\SyncBus`, Shared: true},
				"bus.queue":     {Class: `Lib\Bus\QueueBus`, Shared: true},
				`App\ByDefault`: {Class: `App\ByDefault`, Shared: true, Arguments: []any{service(`Lib\Bus\BusInterface`)}},
				// The locator implements PHP interfaces, which have no file: it is still not a bus
				"app.locator":  {Class: `Lib\Bus\Locator`, Shared: true},
				`App\Explicit`: {Class: `App\Explicit`, Shared: true, Arguments: []any{service("bus.queue"), service("app.locator")}},
				`App\Both`:     {Class: `App\Both`, Shared: true, Arguments: []any{service("bus.sync"), service("bus.queue")}},
				`App\Unlisted`: {Class: `App\Unlisted`, Shared: true},
			},
			Aliases: map[string]interface{}{`Lib\Bus\BusInterface`: "bus.sync"},
		},
		ClassToFile: map[string]string{
			`Lib\Bus\SyncBus`: syncBus, `Lib\Bus\QueueBus`: queueBus, `Lib\Bus\Locator`: locator,
			`App\ByDefault`: byDefault, `App\Explicit`: explicit, `App\Both`: both, `App\Unlisted`: unlisted,
		},
	}

	cases := []struct {
		path, want, why string
	}{
		{byDefault, "", "the default service of the interface only calls the closure"},
		{explicit, "ERROR", "the queue bus is injected instead of the default one, and it stores the closure"},
		{both, "WARNING", "two buses are injected: which one the property holds is not known"},
		{unlisted, "", "no argument says otherwise: the default service is used"},
	}
	for _, c := range cases {
		findings, err := aud.Audit(c.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		results := []symbol.AuditStatus{{FilePath: c.path, Findings: findings}}
		aud.ResolveClosureLeaks(results)
		if got := closureLeaks(results[0].Findings)["handle"]; got != c.want {
			t.Errorf("%s: got %q, want %q (%s)", filepath.Base(c.path), got, c.want, c.why)
		}
	}
}

// A class the container map does not list is located through the Composer autoloader.
func TestComposerClassFile(t *testing.T) {
	root := t.TempDir()
	mkdir := func(parts ...string) string {
		dir := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	composer := mkdir("vendor", "composer")
	lib := mkdir("vendor", "acme", "lib", "src", "Util")
	app := mkdir("src", "Service")
	legacy := mkdir("legacy")
	for path, code := range map[string]string{
		filepath.Join(composer, "autoload_psr4.php"): "<?php\n\n$vendorDir = dirname(__DIR__);\n$baseDir = dirname($vendorDir);\n\nreturn array(\n" +
			"    'Acme\\\\Lib\\\\' => array($vendorDir . '/acme/lib/src'),\n" +
			"    'Acme\\\\' => array($vendorDir . '/acme/other'),\n" +
			"    'App\\\\' => array($baseDir . '/src'),\n);\n",
		filepath.Join(composer, "autoload_classmap.php"): "<?php\nreturn array(\n    'Legacy_Tool' => $baseDir . '/legacy/Tool.php',\n);\n",
		filepath.Join(lib, "Runner.php"):                 "<?php namespace Acme\\Lib\\Util; final class Runner { public static function run(callable $c) { return $c(); } }",
		filepath.Join(app, "Mailer.php"):                 "<?php namespace App\\Service; class Mailer {}",
		filepath.Join(legacy, "Tool.php"):                "<?php class Legacy_Tool {}",
	} {
		if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	aud := NewAuditor(config.Config{})
	aud.Symfony = &SymfonyBridge{Root: root, ClassToFile: map[string]string{}}
	for class, want := range map[string]string{
		`Acme\Lib\Util\Runner`:  filepath.Join(lib, "Runner.php"),
		`App\Service\Mailer`:    filepath.Join(app, "Mailer.php"),
		`Legacy_Tool`:           filepath.Join(legacy, "Tool.php"),
		`Acme\Lib\Util\Missing`: "",
		`Other\Thing`:           "",
	} {
		if got := aud.classFile(class); got != want {
			t.Errorf("%s: got %q, want %q", class, got, want)
		}
	}
	if info, ok := aud.classCallables(`Acme\Lib\Util\Runner`); !ok || !info.Concrete {
		t.Errorf("the class located through Composer must be readable: %+v", info)
	}
}

// array_map() called unqualified in a namespace is the project's own function when a file
// Composer always loads declares one there.
func TestResolveClosureLeaks_ShadowedBuiltin(t *testing.T) {
	audit := func(withShadow bool) string {
		root := t.TempDir()
		composer := filepath.Join(root, "vendor", "composer")
		if err := os.MkdirAll(composer, 0o755); err != nil {
			t.Fatal(err)
		}
		files := map[string]string{
			filepath.Join(root, "Mapper.php"): `<?php
namespace App;
final class Mapper {
    public function each(callable $work, array $items): array { return array_map($work, $items); }
}`,
			filepath.Join(root, "Consumer.php"): `<?php
namespace App;
class Consumer {
    public function __construct(private Mapper $mapper) {}
    public function handle(string $optin): void {
        $this->mapper->each(function () use ($optin) { return $optin; }, []);
    }
}`,
		}
		if withShadow {
			files[filepath.Join(root, "functions.php")] = `<?php
namespace App;
function array_map(callable $callback, array $items): array { $GLOBALS['kept'][] = $callback; return $items; }`
			files[filepath.Join(composer, "autoload_files.php")] = "<?php\n$vendorDir = dirname(__DIR__);\n$baseDir = dirname($vendorDir);\nreturn array(\n    'abc123' => $baseDir . '/functions.php',\n);\n"
		}
		for path, code := range files {
			if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		aud := NewAuditor(config.Config{})
		aud.Symfony = &SymfonyBridge{Root: root, ClassToFile: map[string]string{
			`App\Mapper`: filepath.Join(root, "Mapper.php"), `App\Consumer`: filepath.Join(root, "Consumer.php"),
		}}
		path := filepath.Join(root, "Consumer.php")
		findings, err := aud.Audit(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		results := []symbol.AuditStatus{{FilePath: path, Findings: findings}}
		aud.ResolveClosureLeaks(results)
		return closureLeaks(results[0].Findings)["handle"]
	}
	if got := audit(false); got != "" {
		t.Errorf("array_map() is PHP's: each() only calls the closure, got %q", got)
	}
	if got := audit(true); got != "WARNING" {
		t.Errorf("App\\array_map() is declared by an autoloaded file: each() cannot be trusted, got %q", got)
	}
}

func TestIsRegistrationName(t *testing.T) {
	for _, name := range []string{"addListener", "register", "on", "onKernelRequest", "listen", "subscribe", "push", "setCallback", "extend"} {
		if !isRegistrationName(name) {
			t.Errorf("%s should read as a registration", name)
		}
	}
	for _, name := range []string{"wrapInTransaction", "transactional", "get", "map", "filter", "each", "once", "address", "settle", "run", "process", "visit"} {
		if isRegistrationName(name) {
			t.Errorf("%s should not read as a registration", name)
		}
	}
}
