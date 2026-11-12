package auditor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// injectedMutations gives the methods with a dependency mutation finding.
func injectedMutations(findings []symbol.Finding) map[string]bool {
	reported := map[string]bool{}
	for _, f := range findings {
		if f.Rule == symbol.RuleSingletonMutation {
			reported[f.ContextMethod] = true
		}
	}
	return reported
}

// writeFiles writes PHP files under dir and returns their paths by name.
func writeFiles(t *testing.T, dir string, files map[string]string) map[string]string {
	t.Helper()
	paths := map[string]string{}
	for name, code := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	return paths
}

// A method return type is written as the file declaring it reads it: through its imports.
func TestGetMethodReturnType_ResolvesImports(t *testing.T) {
	paths := writeFiles(t, t.TempDir(), map[string]string{
		"Repository.php": `<?php
namespace Vendor\Bundle\Repository;

use Doctrine\ORM\EntityManagerInterface;
use Doctrine\ORM\Mapping;
use Psr\Log\LoggerInterface as Logger;

class Repository
{
    public function em(): EntityManagerInterface {}
    public function nullable(): ?EntityManagerInterface {}
    public function metadata(): Mapping\ClassMetadata {}
    public function logger(): Logger {}
    public function qualified(): \Doctrine\DBAL\Connection {}
    public function local(): Helper {}
    public function itself(): static {}
    public function scalar(): iterable {}
    public function union(): EntityManagerInterface|Logger {}
}`,
	})
	aud := NewAuditor(config.Config{})
	aud.Symfony = &SymfonyBridge{ClassToFile: map[string]string{`Vendor\Bundle\Repository\Repository`: paths["Repository.php"]}}

	for method, want := range map[string]string{
		"em":        `Doctrine\ORM\EntityManagerInterface`,
		"nullable":  `Doctrine\ORM\EntityManagerInterface`,
		"metadata":  `Doctrine\ORM\Mapping\ClassMetadata`,
		"logger":    `Psr\Log\LoggerInterface`,
		"qualified": `Doctrine\DBAL\Connection`,
		"local":     `Vendor\Bundle\Repository\Helper`,
		"itself":    `Vendor\Bundle\Repository\Repository`,
		"scalar":    "iterable",
		"union":     "EntityManagerInterface|Logger",
	} {
		if got := aud.GetMethodReturnType(`Vendor\Bundle\Repository\Repository`, method); got != want {
			t.Errorf("%s(): got %q, want %q", method, got, want)
		}
	}
}

// A dependency method that only writes to the database, through an object of a safe namespace,
// does not change the dependency's state (#142). A real write, or an inherited method whose
// name says it changes the object, keeps the finding.
func TestDropStatelessDependencyCalls_SafeNamespaceObjects(t *testing.T) {
	dir := t.TempDir()
	paths := writeFiles(t, dir, map[string]string{
		"EntityRepository.php": `<?php
namespace Doctrine\ORM;
class EntityRepository {
    public function getEntityManager(): EntityManagerInterface { return $this->em; }
}`,
		"EntityManagerInterface.php": `<?php
namespace Doctrine\ORM;
use Doctrine\DBAL\Connection;
interface EntityManagerInterface {
    public function getConnection(): Connection;
    public function persist(object $o): void;
    public function flush(): void;
}`,
		"Connection.php": `<?php
namespace Doctrine\DBAL;
class Connection { public function insert(string $t, array $d): int { return 1; } }`,
		"ArrayCollection.php": `<?php
namespace Doctrine\Common\Collections;
class ArrayCollection {
    private array $elements = [];
    public function add($e): void { $this->elements[] = $e; }
    public function removeElement($e): bool { return true; }
}`,
		"TurnRepository.php": `<?php
namespace App\Repository;
use Doctrine\ORM\EntityRepository;
class TurnRepository extends EntityRepository {
    private array $seen = [];
    public function appendIfAbsent(object $turn): bool {
        $this->getEntityManager()->getConnection()->insert('turns', ['id' => 1]);
        return true;
    }
    public function addTurn(object $turn): void {
        $this->getEntityManager()->persist($turn);
        $this->getEntityManager()->flush();
    }
    public function addSeen(object $turn): void { $this->seen[] = $turn; }
}`,
		"QueueRepository.php": `<?php
namespace App\Repository;
use Doctrine\ORM\EntityRepository;
class QueueRepository extends EntityRepository {
    private Mutable $queue;
    public function getQueue(): \Doctrine\Common\Collections\ArrayCollection|\App\Repository\Mutable { return $this->queue; }
    public function appendItem(object $item): void { $this->getQueue()->add($item); }
}`,
		"TurnStore.php": `<?php
namespace App\Repository;
use Doctrine\DBAL\Connection;
class TurnStore {
    public function __construct(private Connection $connection) {}
    public function appendTurn(object $turn): void { $this->connection->insert('turns', ['id' => 1]); }
}`,
		"Cart.php": `<?php
namespace App\Repository;
use Doctrine\Common\Collections\ArrayCollection;
class Cart extends ArrayCollection {}`,
		"ChatCompletion.php": `<?php
namespace App\Chat;
use App\Repository\Cart;
use App\Repository\QueueRepository;
use App\Repository\TurnRepository;
use App\Repository\TurnStore;
class ChatCompletion {
    public function __construct(private TurnRepository $turns, private TurnStore $store, private Cart $cart, private QueueRepository $queues) {}
    public function viaEntityManagerConnection(object $turn): void { $this->turns->appendIfAbsent($turn); }
    public function viaPersistFlush(object $turn): void { $this->turns->addTurn($turn); }
    public function viaInjectedConnection(object $turn): void { $this->store->appendTurn($turn); }
    public function realWrite(object $turn): void { $this->turns->addSeen($turn); }
    public function inheritedAdd(object $item): void { $this->cart->add($item); }
    public function inheritedRemoveElement(object $item): void { $this->cart->removeElement($item); }
    public function viaUnionGetter(object $item): void { $this->queues->appendItem($item); }
}`,
	})
	aud := NewAuditor(config.Config{SafeNamespaces: []string{`Doctrine\`}})
	aud.Symfony = &SymfonyBridge{ClassToFile: map[string]string{
		`Doctrine\ORM\EntityRepository`:               paths["EntityRepository.php"],
		`Doctrine\ORM\EntityManagerInterface`:         paths["EntityManagerInterface.php"],
		`Doctrine\DBAL\Connection`:                    paths["Connection.php"],
		`Doctrine\Common\Collections\ArrayCollection`: paths["ArrayCollection.php"],
		`App\Repository\TurnRepository`:               paths["TurnRepository.php"],
		`App\Repository\TurnStore`:                    paths["TurnStore.php"],
		`App\Repository\Cart`:                         paths["Cart.php"],
		`App\Repository\QueueRepository`:              paths["QueueRepository.php"],
		`App\Chat\ChatCompletion`:                     paths["ChatCompletion.php"],
	}}

	// The vendor files are not audited, as in a project that ignores its vendors
	var results []symbol.AuditStatus
	for _, name := range []string{"ChatCompletion.php", "TurnRepository.php", "TurnStore.php", "Cart.php", "QueueRepository.php"} {
		findings, err := aud.Audit(paths[name], nil)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, symbol.AuditStatus{FilePath: paths[name], Findings: findings})
	}
	before := injectedMutations(results[0].Findings)
	aud.DropStatelessDependencyCalls(results)
	after := injectedMutations(results[0].Findings)

	cases := []struct {
		method string
		kept   bool
		why    string
	}{
		{"viaEntityManagerConnection", false, "appendIfAbsent() inserts through the entity manager's connection"},
		{"viaPersistFlush", false, "addTurn() persists and flushes through the entity manager"},
		{"viaInjectedConnection", false, "appendTurn() inserts through an injected Doctrine connection"},
		{"realWrite", true, "addSeen() writes $this->seen"},
		{"inheritedAdd", true, "Cart inherits add() from a class igor does not read: its name decides"},
		{"inheritedRemoveElement", true, "removeElement() is a mutator name, however the call is spelled"},
		{"viaUnionGetter", true, "getQueue() may return an App object: a union is not a safe namespace"},
	}
	for _, c := range cases {
		if !before[c.method] {
			t.Errorf("%s: expected the name-based finding before the drop", c.method)
		}
		if after[c.method] != c.kept {
			t.Errorf("%s: kept = %v, want %v (%s)", c.method, after[c.method], c.kept, c.why)
		}
	}
}
