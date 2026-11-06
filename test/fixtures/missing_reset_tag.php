<?php

namespace App\Service;

use Igor\WorkerSafe;
use Symfony\Contracts\Service\ResetInterface;
use Symfony\Contracts\Service\ResetInterface as ResetContract;

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

// Not reported: the only property clear() rewrites is marked worker-safe
class SafeCounter
{
    #[WorkerSafe]
    private int $count = 0;

    public function increment(): void
    {
        $this->count++;
    }

    public function clear(): void
    {
        $this->count = 0;
    }
}

// Not reported: clear() needs an argument, so services_resetter cannot call it
class KeyedStore
{
    private array $items = [];

    public function set(string $key, mixed $value): void
    {
        $this->items[$key] = $value;
    }

    public function clear(string $key): void
    {
        $this->items[$key] = null;
    }
}

// Not reported: the writes of make() belong to the anonymous class, not to the service
class ItemsFactory
{
    private array $items = [];

    public function make(): object
    {
        return new class {
            private array $items = [];

            public function add(string $item): void
            {
                $this->items[] = $item;
            }
        };
    }

    public function clear(): void
    {
        $this->items = [];
    }
}

// Reported: implements ResetInterface through an alias and delegates its reset
class AliasedReset implements ResetContract
{
    public function __construct(private object $pool)
    {
    }

    public function reset(): void
    {
        $this->pool->reset();
    }
}

// Not reported: push() empties its buffer in finally, and the client is a lazy initializer
class FinallyBuffer
{
    private array $items = [];
    private ?object $client = null;

    public function push(string $item): void
    {
        try {
            $this->items[] = $item;
            $this->client ??= new \stdClass();
        } finally {
            $this->items = [];
        }
    }

    public function clear(): void
    {
        $this->items = [];
        $this->client = null;
    }
}

// Reported: the finally of the anonymous class clears its own items, not the service's
class AnonymousCleanup
{
    private array $items = [];

    public function add(string $item): object
    {
        $this->items[] = $item;

        return new class {
            private array $items = [];

            public function run(): void
            {
                try {
                    $this->items[] = 'run';
                } finally {
                    $this->items = [];
                }
            }
        };
    }

    public function clear(): void
    {
        $this->items = [];
    }
}
