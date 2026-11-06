<?php

namespace App\EventListener;

use Doctrine\Common\Collections\ArrayCollection as Collection;
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

// Reported: the finally only covers the first write, the append after the try survives the event
class PartlyCoveredListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        try {
            $this->pending[] = $args; // reported
        } finally {
            $this->pending = [];
        }
        $this->pending[] = $args;
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Not reported: each try/finally block clears what it collected
class TwoPhaseListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        try {
            $this->pending[] = $args; // not reported
        } finally {
            $this->pending = [];
        }
        try {
            $this->pending[] = $args;
        } finally {
            $this->pending = [];
        }
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Reported: the empty collection is created through an import alias
class AliasedCollectionListener
{
    private Collection $pending;

    public function onFlush(OnFlushEventArgs $args): void
    {
        $this->pending->add($args);
        $this->pending[] = $args; // reported
    }

    public function postFlush(): void
    {
        $this->pending = new Collection();
    }
}

// Reported: the callback appends after the finally cleared the property
class CallbackListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        try {
            $collect = function (object $entity): void {
                $this->pending[] = $entity; // reported
            };
        } finally {
            $this->pending = [];
        }
        $collect($args);
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Reported: an ignored write in onFlush does not hide the collection in prePersist
class IgnoredEventListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        $this->pending[] = $args; // @igor-ignore
    }

    public function prePersist(object $args): void
    {
        $this->pending[] = $args; // reported
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Not reported: the callbacks run inside the try, before the finally clears the property
class SyncCallbackListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        try {
            array_map(fn (object $entity) => $this->pending[] = $entity, [$args]); // not reported
            (function () use ($args): void {
                $this->pending[] = $args;
            })();
        } finally {
            $this->pending = [];
        }
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Reported: a generator closure only runs when it is advanced, after the finally
class GeneratorListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        try {
            $items = (function () use ($args): \Generator {
                $this->pending[] = $args; // reported
                yield $args;
            })();
        } finally {
            $this->pending = [];
        }
        iterator_to_array($items);
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Reported: array_reduce() only calls its second argument, the initial value is returned as is
class ReduceInitialListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        try {
            $later = array_reduce([], fn ($carry) => $carry, function () use ($args): void {
                $this->pending[] = $args; // reported
            });
        } finally {
            $this->pending = [];
        }
        $later();
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}

// Not reported: the named callback argument runs inside the try
class NamedCallbackListener
{
    private array $pending = [];

    public function onFlush(OnFlushEventArgs $args): void
    {
        try {
            array_map(array: [$args], callback: fn (object $entity) => $this->pending[] = $entity); // not reported
        } finally {
            $this->pending = [];
        }
    }

    public function postFlush(): void
    {
        $this->pending = [];
    }
}
