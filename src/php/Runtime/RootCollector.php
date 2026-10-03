<?php

namespace IgorPhp\IgorBundle\Runtime;

use Symfony\Component\DependencyInjection\Container;
use Symfony\Component\HttpKernel\KernelInterface;

/**
 * Lists the service instances that already exist in the container. It never instantiates one.
 */
class RootCollector
{
    /**
     * @param string[] $ignoredNamespaces
     */
    public function __construct(
        private object $container,
        private ServiceSnapshotter $snapshotter,
        private array $ignoredNamespaces = [],
    ) {
    }

    /**
     * @return array{0: array<string, object>, 1: array<int, string>} watched roots by service id, and
     *                                                               the object id => service id boundary map
     */
    public function collect(): array
    {
        $instances = $this->instances();

        $roots = [];
        $serviceMap = [];
        foreach ($instances as $id => $instance) {
            if (!is_object($instance) || $instance instanceof \Closure) {
                continue;
            }
            $objectId = spl_object_id($instance);
            if (isset($serviceMap[$objectId])) {
                continue;
            }
            $serviceMap[$objectId] = (string) $id;

            if ($this->isWatched($instance)) {
                $roots[(string) $id] = $instance;
            }
        }

        return [$roots, $serviceMap];
    }

    private function instances(): array
    {
        if (!$this->container instanceof Container) {
            return [];
        }

        // $services and $privates are protected: read them from the Container scope
        $read = \Closure::bind(function (): array {
            return $this->services + $this->privates;
        }, $this->container, Container::class);

        return $read();
    }

    private function isWatched(object $instance): bool
    {
        if ($instance instanceof KernelInterface || $instance instanceof Container) {
            return false;
        }

        $class = get_class($instance);
        foreach ($this->ignoredNamespaces as $namespace) {
            if (str_starts_with($class, $namespace)) {
                return false;
            }
        }

        if ((new \ReflectionClass($instance))->isInternal()) {
            return false;
        }

        return !$this->snapshotter->isWorkerSafeClass($class);
    }
}
