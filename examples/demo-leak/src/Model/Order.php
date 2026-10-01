<?php
namespace App\Model;

// Dead definition: registered by the App\Model\ resource but never injected anywhere.
// Symfony's RemoveUnusedDefinitionsPass drops it, so Igor must not audit it.
class Order {
    private array $lines = [];
    public function addLine(string $sku, int $qty): void { $this->lines[$sku] = $qty; }
    public function getLines(): array { return $this->lines; }
}
