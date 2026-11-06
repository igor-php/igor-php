<?php

namespace App\Service;

/**
 * Buffers log lines and has a clear() method, like Symfony's HttpKernel Logger before
 * symfony/symfony#66922: nothing calls clear(), because the service has no kernel.reset tag
 * (autoconfiguration only tags the classes implementing ResetInterface).
 */
class UnwiredClearService
{
    private array $lines = [];

    public function log(string $line): void
    {
        $this->lines[] = $line;
    }

    public function getLines(): array
    {
        return $this->lines;
    }

    public function clear(): void
    {
        $this->lines = [];
    }
}
