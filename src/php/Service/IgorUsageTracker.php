<?php

namespace IgorPhp\IgorBundle\Service;

use Symfony\Contracts\Service\ResetInterface;

/**
 * Accumulates the classes of "leaky" services solicited during the request.
 */
class IgorUsageTracker implements ResetInterface
{
    private array $usedClasses = [];

    public function markAsUsed(string $className): void
    {
        $this->usedClasses[$className] = true;
    }

    public function getUsedClasses(): array
    {
        return array_keys($this->usedClasses);
    }

    public function reset(): void
    {
        // Crucial for FrankenPHP: clear the list for the next request
        $this->usedClasses = [];
    }
}
