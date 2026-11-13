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

final class ShutdownHook
{
    public function __construct(callable $work)
    {
        register_shutdown_function($work);
    }
}

final class Box
{
    public function __construct(private mixed $work)
    {
    }
}

function array_map(callable $callback, array $items): array
{
    $GLOBALS['callbacks'][] = $callback;

    return $items;
}

final class Edges
{
    private mixed $first = null;
    private array $options = [];

    public function once(callable $work): void
    {
        $this->first ??= $work;
    }

    public function merge(callable $work): void
    {
        $this->options += [$work];
    }

    public function viaOptions(callable $work, array $options = []): void
    {
        $options = [$work];
        $this->options = $options;
    }

    public function viaCopy(callable $work): void
    {
        $copy = $this->options;
        $copy[] = $work;
        $this->options = $copy;
    }

    public function spawn(callable $work): void
    {
        new ShutdownHook($work);
    }

    public function box(callable $work): Box
    {
        return new Box($work);
    }

    public function mapped(callable $work): array
    {
        return \App\Closure\array_map($work, []);
    }
}

trait ImmediateRun
{
    public function run(callable $work): mixed
    {
        return $work();
    }
}

trait DeferredRun
{
    private array $later = [];

    public function run(callable $work): mixed
    {
        $this->later[] = $work;

        return null;
    }
}

final class Adapted
{
    use ImmediateRun, DeferredRun {
        DeferredRun::run insteadof ImmediateRun;
    }
}

class KeepingBase
{
    protected array $kept = [];

    public function keep(callable $work): void
    {
        $this->kept[] = $work;
    }
}

trait Delegating
{
    public function relay(callable $work): void
    {
        parent::keep($work);
    }

    public function relayToSelf(callable $work): void
    {
        self::stash($work);
    }
}

final class Relay extends KeepingBase
{
    use Delegating;

    private function stash(callable $work): void
    {
        $this->kept[] = $work;
    }
}

// Applies the modifiers while it is built and keeps only the result, like Valinor's ArrayModifier
final class ArrayModifier
{
    private array $result = [];

    public function __construct(mixed $source, array $modifiers)
    {
        foreach ($modifiers as $key => $modifier) {
            $this->result[$key] = $modifier($source);
        }
    }
}

final class HoldingBox
{
    public function __construct(private \Closure $callback)
    {
    }
}

final class PublicBox
{
    public function __construct(public \Closure $callback)
    {
    }
}

// Copies the closure the box carries: it holds it too
final class Envelope
{
    private \Closure $callback;

    public function __construct(PublicBox $box)
    {
        $this->callback = $box->callback;
    }
}

final class ShutdownBox
{
    public function __construct(callable $callback)
    {
        register_shutdown_function($callback);
    }
}

final class Mapper
{
    public function map(string $class, object $source): object
    {
        return $source;
    }
}

// An interface without implementation igor can read, like Valinor's TreeMapper
interface TreeMapper
{
    public function map(string $signature, mixed $source): mixed;
}

final class ObjectRegistry
{
    private array $objects = [];

    public function keepObject(object $object): void
    {
        $this->objects[] = $object;
    }
}

final class Consumer
{
    public function __construct(
        private Mapper $mapper,
        private TreeMapper $treeMapper,
        private ObjectRegistry $objects,
        private \Sentry\State\HubInterface $hub,
        private Edges $edges,
        private Adapted $adapted,
        private Relay $relay,
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

    // Review cases: writes and calls that are easy to miss
    public function storedWithCoalesceAssign(string $optin): void
    {
        $this->edges->once(function () use ($optin) {
            return $optin;
        });
    }

    public function storedWithPlusAssign(string $optin): void
    {
        $this->edges->merge(function () use ($optin) {
            return $optin;
        });
    }

    public function storedThroughAnotherParameter(string $optin): void
    {
        $this->edges->viaOptions(function () use ($optin) {
            return $optin;
        });
    }

    public function storedThroughACopyOfAProperty(string $optin): void
    {
        $this->edges->viaCopy(function () use ($optin) {
            return $optin;
        });
    }

    public function registeredByAConstructor(string $optin): void
    {
        $this->edges->spawn(function () use ($optin) {
            return $optin;
        });
    }

    public function boxedAndReturned(string $optin): void
    {
        $this->edges->box(function () use ($optin) {
            return $optin;
        });
    }

    public function passedToAShadowingFunction(string $optin): void
    {
        $this->edges->mapped(function () use ($optin) {
            return $optin;
        });
    }

    public function calledOnAdaptedTraits(string $optin): void
    {
        $this->adapted->run(function () use ($optin) {
            return $optin;
        });
    }

    public function storedThroughParentInATrait(string $optin): void
    {
        $this->relay->relay(function () use ($optin) {
            return $optin;
        });
    }

    public function storedThroughSelfInATrait(string $optin): void
    {
        $this->relay->relayToSelf(function () use ($optin) {
            return $optin;
        });
    }

    // The closure is given to a constructor in the arguments of the dependency call
    public function calledByAConstructor(string $optin): void
    {
        $this->mapper->map(Consumer::class, new ArrayModifier([], [
            'rates' => function (array $value) use ($optin) {
                return $optin;
            },
        ]));
    }

    public function calledByAConstructorForAnUnreadableMapper(string $optin): void
    {
        $this->treeMapper->map(Consumer::class, new ArrayModifier([], [
            'rates' => static function (array $value) use ($optin) {
                return $optin;
            },
        ]));
    }

    public function heldByAnObjectForAnUnreadableMapper(string $optin): void
    {
        $this->treeMapper->map(Consumer::class, new HoldingBox(function () use ($optin) {
            return $optin;
        }));
    }

    public function heldByAnObjectTheCallDrops(string $optin): void
    {
        $this->mapper->map(Consumer::class, new HoldingBox(function () use ($optin) {
            return $optin;
        }));
    }

    public function heldByAnObjectTheCallKeeps(string $optin): void
    {
        $this->objects->keepObject(new HoldingBox(function () use ($optin) {
            return $optin;
        }));
    }

    public function nestedWrapperTheCallKeeps(string $optin): void
    {
        $this->objects->keepObject(new Envelope(new PublicBox(function () use ($optin) {
            return $optin;
        })));
    }

    public function nestedWrapperTheCallDrops(string $optin): void
    {
        $this->mapper->map(Consumer::class, new Envelope(new PublicBox(function () use ($optin) {
            return $optin;
        })));
    }

    public function registeredByAConstructorAtTheCallSite(string $optin): void
    {
        $this->mapper->map(Consumer::class, new ShutdownBox(function () use ($optin) {
            return $optin;
        }));
    }

    public function unreadableConstructorTheCallKeeps(string $optin): void
    {
        $this->objects->keepObject(new \Vendor\Unknown\Thing(function () use ($optin) {
            return $optin;
        }));
    }

    public function unreadableConstructorTheCallDrops(string $optin): void
    {
        $this->mapper->map(Consumer::class, new \Vendor\Unknown\Thing(function () use ($optin) {
            return $optin;
        }));
    }

    public function sentryScope(string $optin): void
    {
        $this->hub->withScope(function () use ($optin) {
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
