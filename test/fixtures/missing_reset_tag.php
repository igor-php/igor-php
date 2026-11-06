<?php

namespace App\Service;

use Symfony\Contracts\Service\ResetInterface;

// Reported: clear() empties what log() fills, but nothing calls it
class BufferingLogger
{
    private array $logs = [];

    public function log(string $message): void
    {
        $this->logs[] = $message;
    }

    public function clear(): void
    {
        $this->logs = [];
    }
}

// Reported: implements ResetInterface, but its definition has no kernel.reset tag
class SkolemConverter implements ResetInterface
{
    private array $map = [];

    public function convert(string $iri): string
    {
        return $this->map[$iri] ??= uniqid();
    }

    public function reset(): void
    {
        $this->map = [];
    }
}

// Not reported: clear() empties a property that only the constructor writes
class ConfiguredRegistry
{
    private array $entries;

    public function __construct(array $entries)
    {
        $this->entries = $entries;
    }

    public function clear(): void
    {
        $this->entries = [];
    }
}

// Not reported: clear() delegates and rewrites nothing
class CacheFacade
{
    private int $hits = 0;

    public function __construct(private object $pool)
    {
    }

    public function get(string $key): mixed
    {
        $this->hits++;
        return $this->pool->get($key);
    }

    public function clear(): void
    {
        $this->pool->clear();
    }
}

// Not reported: a private clear() is no reset entry point
class PrivateCleanup
{
    private array $items = [];

    public function add(string $item): void
    {
        $this->items[] = $item;
    }

    private function clear(): void
    {
        $this->items = [];
    }
}

// Not reported: the container resets it (the engine says so)
class WiredLogger
{
    private array $logs = [];

    public function log(string $message): void
    {
        $this->logs[] = $message;
    }

    public function clear(): void
    {
        $this->logs = [];
    }
}
