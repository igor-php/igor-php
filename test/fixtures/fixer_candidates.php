<?php

namespace App\Fixer;

use Symfony\Component\HttpFoundation\Request;
use Symfony\Contracts\Service\ResetInterface;

// Safe: the missing assignment is added to reset().
class IncompleteResetService implements ResetInterface
{
    private int $count = 0;

    public function run(): void
    {
        $this->count++;
    }

    public function reset(): void
    {
    }
}

// Risky: the class becomes resettable.
class StatefulService
{
    private array $items = [];

    public function add(string $item): void
    {
        $this->items[] = $item;
    }
}

// Risky: superglobal reads move to the Request and gc_disable() is removed.
class RequestService
{
    public function read(Request $request): string
    {
        gc_disable();

        return $_GET['id'] ?? '';
    }
}

// Never fixed: requires a human decision.
class ManualService
{
    public function stop(): void
    {
        exit(1);
    }
}
