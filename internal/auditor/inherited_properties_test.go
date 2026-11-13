package auditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// A property declared in a parent class or a trait has the type declared there: ConstraintValidator's
// $context and DenormalizerAwareTrait's $denormalizer are Symfony objects, and what they return is
// not a shared service.
func TestInheritedPropertyTypes(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"ConstraintValidator.php": `<?php
namespace Symfony\Component\Validator;
use Symfony\Component\Validator\Context\ExecutionContextInterface;
abstract class ConstraintValidator {
    protected ExecutionContextInterface $context;
    public function initialize(ExecutionContextInterface $context): void { $this->context = $context; }
}`,
		"ExecutionContextInterface.php": `<?php
namespace Symfony\Component\Validator\Context;
interface ExecutionContextInterface {
    public function addViolation(string $message, array $params = []): void;
    public function getObject(): ?object;
}`,
		"DenormalizerAwareTrait.php": `<?php
namespace Symfony\Component\Serializer\Normalizer;
trait DenormalizerAwareTrait {
    protected DenormalizerInterface $denormalizer;
    public function setDenormalizer(DenormalizerInterface $denormalizer): void { $this->denormalizer = $denormalizer; }
}`,
		"DenormalizerInterface.php": `<?php
namespace Symfony\Component\Serializer\Normalizer;
interface DenormalizerInterface { public function denormalize(mixed $data, string $type): mixed; }`,
		"Registry.php": `<?php
namespace App\Support;
class Registry { private array $values = []; public function setValue(mixed $v): void { $this->values[] = $v; } }`,
		"BaseService.php": `<?php
namespace App\Support;
abstract class BaseService { public function __construct(protected Registry $registry) {} }`,
		"App.php": `<?php
namespace App\Validator;

use App\Support\BaseService;
use Symfony\Component\Serializer\Normalizer\DenormalizerAwareTrait;

final class ParentValidator extends \Symfony\Component\Validator\ConstraintValidator
{
    public function validate(mixed $value): void
    {
        $this->context->addViolation('invalid');
        $entity = $this->context->getObject();
        $entity->setParent(null);
    }
}

final class TreeDenormalizer
{
    use DenormalizerAwareTrait;

    public function denormalize(mixed $data): mixed
    {
        $entity = $this->denormalizer->denormalize($data, 'Tree');
        $entity->setParent(null);

        return $entity;
    }
}

final class Counter extends BaseService
{
    public function bump(): void
    {
        $this->registry->setValue(1);
    }
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

	aud := NewAuditor(config.Config{SafeNamespaces: []string{`Symfony\`}})
	aud.Symfony = &SymfonyBridge{
		Container: &symbol.SymfonyContainer{Definitions: map[string]symbol.SymfonyService{
			`App\Validator\ParentValidator`:  {Class: `App\Validator\ParentValidator`, Shared: true},
			`App\Validator\TreeDenormalizer`: {Class: `App\Validator\TreeDenormalizer`, Shared: true},
			`App\Validator\Counter`:          {Class: `App\Validator\Counter`, Shared: true},
			`App\Support\Registry`:           {Class: `App\Support\Registry`, Shared: true},
		}},
		ClassToFile: map[string]string{
			`Symfony\Component\Validator\ConstraintValidator`:                paths["ConstraintValidator.php"],
			`Symfony\Component\Validator\Context\ExecutionContextInterface`:  paths["ExecutionContextInterface.php"],
			`Symfony\Component\Serializer\Normalizer\DenormalizerAwareTrait`: paths["DenormalizerAwareTrait.php"],
			`Symfony\Component\Serializer\Normalizer\DenormalizerInterface`:  paths["DenormalizerInterface.php"],
			`App\Support\Registry`:           paths["Registry.php"],
			`App\Support\BaseService`:        paths["BaseService.php"],
			`App\Validator\ParentValidator`:  paths["App.php"],
			`App\Validator\TreeDenormalizer`: paths["App.php"],
			`App\Validator\Counter`:          paths["App.php"],
		},
	}

	if got := aud.GetPropertyType(`App\Validator\ParentValidator`, "context"); got != `Symfony\Component\Validator\Context\ExecutionContextInterface` {
		t.Errorf("$context comes from the parent class (extends written fully qualified), got %q", got)
	}
	if got := aud.GetPropertyType(`App\Validator\TreeDenormalizer`, "denormalizer"); got != `Symfony\Component\Serializer\Normalizer\DenormalizerInterface` {
		t.Errorf("$denormalizer comes from the trait, got %q", got)
	}
	if got := aud.GetPropertyType(`App\Validator\Counter`, "registry"); got != `App\Support\Registry` {
		t.Errorf("$registry is promoted in the parent constructor, got %q", got)
	}

	findings, err := aud.Audit(paths["App.php"], nil)
	if err != nil {
		t.Fatal(err)
	}
	reported := map[string]bool{}
	for _, f := range findings {
		if f.Rule == symbol.RuleSingletonMutation {
			reported[f.ContextClass+"::"+f.ContextMethod] = true
		}
	}
	for method, want := range map[string]bool{
		`App\Validator\ParentValidator::validate`:     false,
		`App\Validator\TreeDenormalizer::denormalize`: false,
		`App\Validator\Counter::bump`:                 true,
	} {
		if reported[method] != want {
			t.Errorf("%s: reported = %v, want %v", method, reported[method], want)
		}
	}
}

// self in a trait names the class using it; a conflict between traits is solved as PHP does
// (insteadof, as); a trait of the class's namespace used without import is not mistaken for the
// "use" line of a class body; a local named like an inherited property keeps the type of what it
// was assigned.
func TestTraitAndLocalTypes(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"Traits.php": `<?php
namespace App\Support;
trait CurrentTrait { public function current(): self { return $this; } }
trait LoggerTrait { public function handler() { return new Logger(); } }
trait CacheTrait { public function handler(): Cache { return new Cache(); } }
trait HelperTrait { protected Helper $helper; }`,
		"Registry.php": `<?php
namespace App\Support;
class Before
{
    use HelperTrait;
}
class Registry
{
    use CurrentTrait;
    use LoggerTrait, CacheTrait {
        LoggerTrait::handler insteadof CacheTrait;
        CacheTrait::handler as cacheHandler;
    }
    use HelperTrait;
    private array $values = [];
    public function setValue(mixed $v): void { $this->values[] = $v; }
}`,
		"ConstraintValidator.php": `<?php
namespace Symfony\Component\Validator;
abstract class ConstraintValidator { protected \Symfony\Component\Validator\Context\ExecutionContextInterface $context; }`,
		"App.php": `<?php
namespace App\Validator;

final class LocalValidator extends \Symfony\Component\Validator\ConstraintValidator
{
    public function __construct(private $untypedRegistry) {}

    public function validate(mixed $value): void
    {
        $context = $this->untypedRegistry;
        $context->setValue($value);
    }
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

	aud := NewAuditor(config.Config{SafeNamespaces: []string{`Symfony\`}})
	aud.Symfony = &SymfonyBridge{
		Container: &symbol.SymfonyContainer{Definitions: map[string]symbol.SymfonyService{
			`App\Validator\LocalValidator`: {Class: `App\Validator\LocalValidator`, Shared: true},
		}},
		ClassToFile: map[string]string{
			`App\Support\Registry`:                            paths["Registry.php"],
			`App\Support\CurrentTrait`:                        paths["Traits.php"],
			`App\Support\LoggerTrait`:                         paths["Traits.php"],
			`App\Support\CacheTrait`:                          paths["Traits.php"],
			`App\Support\HelperTrait`:                         paths["Traits.php"],
			`Symfony\Component\Validator\ConstraintValidator`: paths["ConstraintValidator.php"],
			`App\Validator\LocalValidator`:                    paths["App.php"],
		},
	}

	if got := aud.GetMethodReturnType(`App\Support\Registry`, "current"); got != `App\Support\Registry` {
		t.Errorf("self in a trait is the class using it, got %q", got)
	}
	if got := aud.GetMethodReturnType(`App\Support\Registry`, "handler"); got != "" {
		t.Errorf("handler() is LoggerTrait's (insteadof), which has no return type, got %q", got)
	}
	if got := aud.GetMethodReturnType(`App\Support\Registry`, "cacheHandler"); got != `App\Support\Cache` {
		t.Errorf("cacheHandler() is CacheTrait::handler renamed, got %q", got)
	}
	if got := aud.GetPropertyType(`App\Support\Registry`, "helper"); got != `App\Support\Helper` {
		t.Errorf("HelperTrait is in the namespace of Registry, and the use line of Before is not an import, got %q", got)
	}

	findings, err := aud.Audit(paths["App.php"], nil)
	if err != nil {
		t.Fatal(err)
	}
	reported := false
	for _, f := range findings {
		if f.Rule == symbol.RuleSingletonMutation && f.ContextMethod == "validate" {
			reported = true
		}
	}
	if !reported {
		t.Errorf("$context is a local holding $this->untypedRegistry, not the inherited $this->context: %+v", findings)
	}
}

// A property the class declares itself, typed or not, hides the parent's, and a private property
// of a parent is not inherited: $this->context of the subclass is not the parent's
// ExecutionContextInterface. Grouped imports name the class they import.
func TestOwnPropertiesAndGroupedImports(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"Base.php": `<?php
namespace App\Support;
use Symfony\Component\Validator\Context\ExecutionContextInterface;
abstract class Base
{
    private ExecutionContextInterface $context;
    private ExecutionContextInterface $hidden;
    protected ExecutionContextInterface $shared;
}`,
		"Child.php": `<?php
namespace App\Support;
class Child extends Base
{
    private $context;
}`,
		"CurrentTrait.php": `<?php
namespace App\Support;
use App\State\{Cache, Registry as SharedRegistry};
trait CurrentTrait
{
    public function current(): SharedRegistry { return $this->registry; }
    public function cache(): Cache { return $this->cache; }
}`,
		"Holder.php": `<?php
namespace App\Support;
class Holder { use CurrentTrait; }`,
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
		Container: &symbol.SymfonyContainer{Definitions: map[string]symbol.SymfonyService{}},
		ClassToFile: map[string]string{
			`App\Support\Base`:         paths["Base.php"],
			`App\Support\Child`:        paths["Child.php"],
			`App\Support\CurrentTrait`: paths["CurrentTrait.php"],
			`App\Support\Holder`:       paths["Holder.php"],
		},
	}

	for prop, want := range map[string]string{
		"context": "", // declared untyped by Child
		"hidden":  "", // private to Base
		"shared":  `Symfony\Component\Validator\Context\ExecutionContextInterface`,
	} {
		if got := aud.GetPropertyType(`App\Support\Child`, prop); got != want {
			t.Errorf("Child::$%s: got %q, want %q", prop, got, want)
		}
	}
	if got := aud.GetPropertyType(`App\Support\Base`, "context"); got != `Symfony\Component\Validator\Context\ExecutionContextInterface` {
		t.Errorf("Base reads its own private $context, got %q", got)
	}
	for method, want := range map[string]string{
		"current": `App\State\Registry`,
		"cache":   `App\State\Cache`,
	} {
		if got := aud.GetMethodReturnType(`App\Support\Holder`, method); got != want {
			t.Errorf("Holder::%s(): got %q, want %q", method, got, want)
		}
	}
}

func TestExpandImport(t *testing.T) {
	for stmt, want := range map[string][]string{
		`use Foo\Bar;`:                                      {`use Foo\Bar;`},
		"use \\Foo\\Bar as B, Baz\\Qux;":                    {`use Foo\Bar as B;`, `use Baz\Qux;`},
		"use App\\State\\{\n  Registry,\n  Cache as C,\n};": {`use App\State\Registry;`, `use App\State\Cache as C;`},
		`use function App\helper;`:                          nil,
		`use const App\LIMIT;`:                              nil,
	} {
		got := expandImport(stmt)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("expandImport(%q) = %q, want %q", stmt, got, want)
		}
	}
}
