<?php

namespace App\Services;

// Same code as StatefulService, but NOT listed in octane.warm: it is first resolved
// inside the per-request sandbox, so Octane throws it away after every request.
class LazySingletonService {
    private array $cache = [];
    public function addData(string $k, string $v): void { $this->cache[$k] = $v; }
    public function getData(): array { return $this->cache; }
}
