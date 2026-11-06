<?php

namespace App\EventListener;

use Doctrine\ORM\Event\OnFlushEventArgs;
use Igor\WorkerSafe;
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

// Reported: the clear only runs when $startFresh is true
class ConditionalClearListener
{
    private array $pending = [];
    private bool $startFresh = false;

    public function onFlush(OnFlushEventArgs $args): void
    {
        if ($this->startFresh) {
            $this->pending = [];
        }
        $this->pending[] = $args; // reported
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Not reported: onFlush clears what it collected in its own finally block
class FinallyListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        try {
            $this->pending[] = $args; // not reported
            $this->process($this->pending);
        } finally {
            $this->pending = [];
        }
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }

    private function process(array $pending): void
    {
    }
}

// Not reported: the event is marked worker-safe
class SafeEventListener
{
    private array $pending = [];

    #[WorkerSafe]
    public function onFlush(OnFlushEventArgs $args): void
    {
        $this->pending[] = $args; // not reported
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Not reported: the writes belong to an anonymous helper, not to the listener
class HelperListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): object
    {
        return new class {
            private array $pending = [];

            public function add(object $entity): void
            {
                $this->pending[] = $entity; // not reported
            }
        };
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Reported: the finally only clears the collection when $discard is set
class ConditionalFinallyListener
{
    private array $pending = [];
    private bool $discard = false;

    public function onFlush(OnFlushEventArgs $args): void
    {
        try {
            $this->pending[] = $args; // reported
        } finally {
            if ($this->discard) {
                $this->pending = [];
            }
        }
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}
