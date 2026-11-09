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
