package auditor

import (
	"os"
	"path/filepath"
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
