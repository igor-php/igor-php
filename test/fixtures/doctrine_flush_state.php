<?php

namespace App\EventListener;

use Doctrine\ORM\Event\OnFlushEventArgs;
use Symfony\Contracts\Service\ResetInterface;

// Reported: collected in onFlush, cleared only in postFlush
class PublishListener
{
    private array $pending = [];
    private array $deleted = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        foreach ($args->getObjectManager()->getUnitOfWork()->getScheduledEntityInsertions() as $entity) {
            $this->pending[] = $entity; // reported
        }
        foreach ($args->getObjectManager()->getUnitOfWork()->getScheduledEntityDeletions() as $entity) {
            $this->deleted[spl_object_id($entity)] = $entity; // reported
        }
    }

    public function postFlush(): void
    {
        try {
            $this->publish($this->pending, $this->deleted);
        } finally {
            $this->pending = [];
            $this->deleted = [];
        }
    }

    private function publish(array $pending, array $deleted): void
    {
    }
}

// Reported: collected in prePersist
class AuditListener
{
    private array $entries = [];

    public function prePersist(object $args): void
    {
        $this->entries[] = $args; // reported
    }

    public function postFlush(): void
    {
        $this->entries = [];
    }
}

// Not reported: onFlush starts by clearing what a failed commit left
class ClearsFirstListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        $this->pending = [];
        foreach ($args->getObjectManager()->getUnitOfWork()->getScheduledEntityInsertions() as $entity) {
            $this->pending[] = $entity; // not reported
        }
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Not reported: reset between requests
class ResettableListener implements ResetInterface
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        $this->pending[] = $args; // not reported
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }

    public function reset(): void
    {
        $this->pending = [];
    }
}

// Not reported: also cleared by onClear, which runs when the entity manager is cleared
class OnClearListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        $this->pending[] = $args; // not reported
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }

    public function onClear(): void
    {
        $this->pending = [];
    }
}

// Not reported: postFlush does not clear it (the generic mutation finding covers it)
class CounterListener
{
    private int $flushes = 0;

    public function onFlush(OnFlushEventArgs $args): void
    {
        $this->flushes++; // not reported
    }

    public function postFlush(): void
    {
        $this->log($this->flushes);
    }

    private function log(int $n): void
    {
    }
}

// Not reported: no postFlush, not a flush listener
class Collector
{
    private array $items = [];

    public function onFlush(object $args): void
    {
        $this->items[] = $args; // not reported
    }

    public function flush(): void
    {
        $this->items = [];
    }
}
