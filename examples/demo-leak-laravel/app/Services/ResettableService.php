<?php
namespace App\Services;
class ResettableService {
    private array $data = [];
    public function add(string $val): void { $this->data[] = $val; }
    public function get(): array { return $this->data; }
}
