<?php

namespace App\Closure;

interface Registry
{
    public function addListener(string $event, callable $listener): void;

    public function visit(callable $visitor): void;
}

final class ListenerRegistry implements Registry
{
    private array $listeners = [];

    public function addListener(string $event, callable $listener): void
    {
        $this->listeners[$event][] = $listener;
    }

    public function visit(callable $visitor): void
    {
        foreach ($this->listeners as $event => $listeners) {
            $visitor($event, $listeners);
        }
    }

    public function keepLater(callable $listener): void
    {
        $this->remember('later', $listener);
    }

    private function remember(string $event, callable $listener): void
    {
        $this->listeners[$event][] = $listener;
    }
}

final class Runner
{
    public static function run(callable $work): mixed
    {
        return $work();
    }
}

final class Transactions
{
    public function wrap(callable $work): mixed
    {
        try {
            return $work($this);
        } finally {
            $this->close();
        }
    }

    public function retry(int $times, callable $work): mixed
    {
        return $this->wrap($work);
    }

    public function viaHelper(callable $work): mixed
    {
        return Runner::run($work);
    }

    private function close(): void
    {
    }
}

abstract class AbstractBus
{
    public function now(callable $work): mixed
    {
        return $work();
    }
}

class OpenBus
{
    public function run(callable $work): mixed
    {
        return $work();
    }
}

class RecordingBus extends OpenBus
{
    private array $recorded = [];

    public function run(callable $work): mixed
    {
        $this->recorded[] = $work;

        return null;
    }
}

trait Deferring
{
    private array $deferred = [];

    public function defer(callable $work): void
    {
        $this->deferred[] = $work;
    }

    public function immediately(callable $work): mixed
    {
        return $work();
    }
}

final class Scheduler
{
    use Deferring;
}

final class Consumer
{
    public function __construct(
        private ListenerRegistry $registry,
        private Registry $contract,
        private Transactions $transactions,
        private AbstractBus $bus,
        private OpenBus $open,
        private Scheduler $scheduler,
        private \Doctrine\ORM\EntityManagerInterface $entityManager,
        private \Symfony\Contracts\Cache\CacheInterface $cache,
        private $untyped,
    ) {
    }

    // The method stores the closure: a leak
    public function storedDirectly(string $optin): void
    {
        $this->registry->addListener('response', function () use ($optin) {
            return $optin;
        });
    }

    public function storedThroughPrivateMethod(string $optin): void
    {
        $this->registry->keepLater(function () use ($optin) {
            return $optin;
        });
    }

    public function storedInsideAnArray(string $optin): void
    {
        $this->registry->addListener('response', ['callback' => function () use ($optin) {
            return $optin;
        }]);
    }

    public function storedByATrait(string $optin): void
    {
        $this->scheduler->defer(function () use ($optin) {
            return $optin;
        });
    }

    // The method only calls the closure: no leak
    public function calledOnly(string $optin): void
    {
        $this->transactions->wrap(function () use ($optin) {
            return $optin;
        });
    }

    public function calledThroughAnotherMethod(string $optin): void
    {
        $this->transactions->retry(3, function () use ($optin) {
            return $optin;
        });
    }

    public function calledThroughAStaticHelper(string $optin): void
    {
        $this->transactions->viaHelper(function () use ($optin) {
            return $optin;
        });
    }

    public function calledForEachListener(string $optin): void
    {
        $this->registry->visit(function (string $event) use ($optin) {
            return $event.$optin;
        });
    }

    public function calledByATrait(string $optin): void
    {
        $this->scheduler->immediately(function () use ($optin) {
            return $optin;
        });
    }

    public function knownTransaction(string $optin): void
    {
        $this->entityManager->wrapInTransaction(function () use ($optin) {
            return $optin;
        });
    }

    public function knownCacheCallback(string $optin): void
    {
        $this->cache->get('key', function () use ($optin) {
            return $optin;
        });
    }

    // The method cannot be read: its name decides
    public function registeredThroughAnInterface(string $optin): void
    {
        $this->contract->addListener('response', function () use ($optin) {
            return $optin;
        });
    }

    public function visitedThroughAnInterface(string $optin): void
    {
        $this->contract->visit(function () use ($optin) {
            return $optin;
        });
    }

    public function calledOnAnAbstractClass(string $optin): void
    {
        $this->bus->now(function () use ($optin) {
            return $optin;
        });
    }

    public function calledOnAClassWithASubclass(string $optin): void
    {
        $this->open->run(function () use ($optin) {
            return $optin;
        });
    }

    public function registeredOnAnUntypedProperty(string $optin): void
    {
        $this->untyped->addListener('response', function () use ($optin) {
            return $optin;
        });
    }

    public function calledOnAnUntypedProperty(string $optin): void
    {
        $this->untyped->process(function () use ($optin) {
            return $optin;
        });
    }

    public function calledWithANamedArgument(string $optin): void
    {
        $this->transactions->wrap(work: function () use ($optin) {
            return $optin;
        });
    }
}
