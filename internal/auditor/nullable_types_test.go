package auditor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/igor-php/igor-php/internal/config"
	"github.com/igor-php/igor-php/pkg/symbol"
)

// A nullable declared type names the same class: ?EntityManagerInterface is a dependency of a safe
// namespace, as EntityManagerInterface is. It was resolved as the class "?EntityManagerInterface"
// of the file's namespace.
func TestNullableDependencyTypes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Service.php")
	if err := os.WriteFile(path, []byte(`<?php
namespace App\Service;

use Doctrine\ORM\EntityManagerInterface;

final class Service
{
    public function __construct(
        private EntityManagerInterface $em,
        private ?EntityManagerInterface $maybeEm = null,
        private ?Registry $registry = null,
    ) {}

    public function plain(): void { $this->em->clear(); }
    public function nullable(): void { $this->maybeEm->clear(); }
    public function nullableAlias(): void { $em = $this->maybeEm; $em?->clear(); }
    public function nullableApp(): void { $this->registry->setValue(1); }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	aud := NewAuditor(config.Config{SafeNamespaces: []string{`Doctrine\`}})
	findings, err := aud.Audit(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	reported := map[string]bool{}
	for _, f := range findings {
		if f.Rule == symbol.RuleSingletonMutation {
			reported[f.ContextMethod] = true
		}
	}
	for method, want := range map[string]bool{"plain": false, "nullable": false, "nullableAlias": false, "nullableApp": true} {
		if reported[method] != want {
			t.Errorf("%s: reported = %v, want %v", method, reported[method], want)
		}
	}
}
