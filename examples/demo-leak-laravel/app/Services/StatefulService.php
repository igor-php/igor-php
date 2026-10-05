<?php
namespace App\Services;
class StatefulService {
    private array $cache = [];
    public function addData(string $k, string $v): void { $this->cache[$k] = $v; }
    public function getData(): array { return $this->cache; }
}
