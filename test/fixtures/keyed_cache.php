<?php

namespace App\Service;

class Caches
{
    private array $normalizerCache = [];
    private array $resolved = [];
    private array $items = [];
    private static array $cache = [];

    public function run(string $format, string $type, object $other, string $k): void
    {
        $this->normalizerCache[$format][$type] = $other; // cache [$format][$type]
        $this->resolved[$k] ??= $this->compute($k); // cache [$k]
        self::$cache[$k] = 1; // cache [$k]
        $this->items[$k] = $other; // mutation
        $this->resolved[$k] ??= 0; // mutation
        $this->normalizerCache[] = $other; // mutation
        $this->normalizerCache = []; // mutation
    }

    private function compute(string $k): string
    {
        return $k;
    }
}
