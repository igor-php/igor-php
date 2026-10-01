<?php
namespace App\Internal;

// Dead definition: a private stateful service that no other service references.
// It never exists at runtime, so Igor must not report its mutations.
class OrphanStatefulHelper {
    private array $seen = [];
    public function remember(string $key): void { $this->seen[] = $key; }
}
