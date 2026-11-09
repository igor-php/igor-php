<?php

namespace App\Service;

class ClosureLeakService
{
    private array $listeners = [];

    public function addListener(string $event, callable $listener): void
    {
        $this->listeners[$event][] = $listener;
    }

    public function getListeners(): array
    {
        return $this->listeners;
    }

    /**
     * Calls the callback and returns its result: nothing is kept once the call is over.
     */
    public function runNow(callable $callback): mixed
    {
        return $callback();
    }
}
