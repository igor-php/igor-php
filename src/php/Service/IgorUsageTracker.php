<?php

namespace IgorPhp\IgorBundle\Service;

use Symfony\Contracts\Service\ResetInterface;

/**
 * Accumulates the classes of "leaky" services solicited during the request.
 */
class IgorUsageTracker implements ResetInterface
{
    private array $usedFiles = [];

    public function markAsUsed(string $filePath): void
    {
        $this->usedFiles[$filePath] = true;
    }

    public function getUsedFiles(): array
    {
        return array_keys($this->usedFiles);
    }

    public function reset(): void
    {
        $this->usedFiles = [];
    }
}
