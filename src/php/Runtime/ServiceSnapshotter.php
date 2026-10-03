<?php

namespace IgorPhp\IgorBundle\Runtime;

use IgorPhp\IgorBundle\Attribute\WorkerSafe;

/**
 * Copies the state reachable from an object into a plain value tree, without waking lazy objects.
 *
 * Scalars are kept as-is; every other value becomes an array node carrying a '@t' type key.
 */
class ServiceSnapshotter
{
    /** @var array<string, array<string, true>> */
    private array $workerSafeProps = [];
    /** @var array<string, bool> */
    private array $workerSafeClasses = [];

    /**
     * @param string[] $opaqueNamespaces objects of these namespaces are recorded by class only
     */
    public function __construct(
        private array $opaqueNamespaces = [],
        private int $maxDepth = 12,
        private int $maxNodes = 5000,
    ) {
    }

    /**
     * @param array<int, string> $serviceMap spl_object_id => service id, used as walk boundary
     */
    public function snapshot(object $root, array $serviceMap = []): array
    {
        $seen = [spl_object_id($root) => ''];
        $budget = $this->maxNodes;

        $node = $this->objectNode($root, '', 0, $seen, $serviceMap, $budget);
        foreach ($this->staticState($root, $seen, $serviceMap, $budget) as $name => $value) {
            $node['props'][$name] = $value;
        }

        return $node;
    }

    /**
     * Snapshots a plain value (no root object), e.g. process-wide state.
     */
    public function snapshotValue(mixed $value): mixed
    {
        $seen = [];
        $budget = $this->maxNodes;

        return $this->walk($value, '', 0, $seen, [], $budget);
    }

    private function walk(mixed $value, string $path, int $depth, array &$seen, array $serviceMap, int &$budget): mixed
    {
        if ($value === null || is_scalar($value)) {
            return $value;
        }

        if (--$budget < 0 || $depth > $this->maxDepth) {
            return ['@t' => 'truncated'];
        }

        if (is_array($value)) {
            $items = [];
            foreach ($value as $key => $item) {
                $items[$key] = $this->walk($item, $path . '[' . $key . ']', $depth + 1, $seen, $serviceMap, $budget);
            }

            return ['@t' => 'array', 'n' => count($value), 'items' => $items];
        }

        if (!is_object($value)) {
            // Resources: the handle number changes on every reopen, only the kind is meaningful
            return ['@t' => 'opaque', 'desc' => 'resource(' . get_resource_type($value) . ')'];
        }

        if ($value instanceof \Closure) {
            return $this->closureNode($value, $path, $depth, $seen, $serviceMap, $budget);
        }

        $id = spl_object_id($value);
        if (isset($seen[$id])) {
            return ['@t' => 'ref', 'to' => $seen[$id] === '' ? '(root)' : $seen[$id]];
        }
        if (isset($serviceMap[$id])) {
            return ['@t' => 'service', 'id' => $serviceMap[$id]];
        }
        $seen[$id] = $path;

        if ($value instanceof \UnitEnum) {
            return ['@t' => 'opaque', 'desc' => get_class($value) . '::' . $value->name];
        }
        if ($value instanceof \DateTimeInterface) {
            return ['@t' => 'opaque', 'desc' => get_class($value) . '(' . $value->format('Y-m-d\TH:i:s.uP') . ')'];
        }
        if ($this->isUninitializedLazy($value)) {
            return ['@t' => 'lazy', 'class' => get_class($value)];
        }
        if ($this->isOpaque($value)) {
            return ['@t' => 'opaque', 'desc' => get_class($value)];
        }

        return $this->objectNode($value, $path, $depth, $seen, $serviceMap, $budget);
    }

    private function objectNode(object $object, string $path, int $depth, array &$seen, array $serviceMap, int &$budget): array
    {
        $class = get_class($object);
        $skipped = $this->workerSafeProperties($class);

        $props = [];
        foreach (get_mangled_object_vars($object) as $mangled => $raw) {
            $pos = strrpos($mangled, "\0");
            $name = $pos === false ? $mangled : substr($mangled, $pos + 1);
            if (isset($skipped[$name])) {
                continue;
            }
            $props[$name] = $this->walk($raw, $path . '->' . $name, $depth + 1, $seen, $serviceMap, $budget);
        }

        if ($object instanceof \ArrayObject || $object instanceof \ArrayIterator) {
            $props['[]'] = $this->walk($object->getArrayCopy(), $path . '->[]', $depth + 1, $seen, $serviceMap, $budget);
        }

        return ['@t' => 'object', 'class' => $class, 'props' => $props];
    }

    private function closureNode(\Closure $closure, string $path, int $depth, array &$seen, array $serviceMap, int &$budget): array
    {
        $ref = new \ReflectionFunction($closure);
        $file = $ref->getFileName();

        return [
            '@t' => 'closure',
            'at' => ($file === false ? 'internal' : $file) . ':' . $ref->getStartLine(),
            'uses' => $this->walk($ref->getStaticVariables(), $path . '{use}', $depth + 1, $seen, $serviceMap, $budget),
        ];
    }

    /**
     * Static properties and method-local static variables of the root's class hierarchy.
     */
    private function staticState(object $root, array &$seen, array $serviceMap, int &$budget): array
    {
        $state = [];
        $rootClass = new \ReflectionClass($root);

        for ($class = $rootClass; $class !== false && !$class->isInternal(); $class = $class->getParentClass()) {
            $prefix = $class->getName() === $rootClass->getName() ? '' : $class->getShortName();
            $skipped = $this->workerSafeProperties($class->getName());

            foreach ($class->getProperties(\ReflectionProperty::IS_STATIC) as $prop) {
                if ($prop->getDeclaringClass()->getName() !== $class->getName() || isset($skipped[$prop->getName()])) {
                    continue;
                }
                if (!$prop->isInitialized()) {
                    continue;
                }
                $key = $prefix . '::$' . $prop->getName();
                $state[$key] = $this->walk($prop->getValue(), $key, 1, $seen, $serviceMap, $budget);
            }

            foreach ($class->getMethods() as $method) {
                if ($method->getDeclaringClass()->getName() !== $class->getName() || $method->isAbstract()) {
                    continue;
                }
                if ($method->getAttributes(WorkerSafe::class) !== []) {
                    continue;
                }
                foreach ($method->getStaticVariables() as $name => $value) {
                    $key = $prefix . '::' . $method->getName() . '() static $' . $name;
                    $state[$key] = $this->walk($value, $key, 1, $seen, $serviceMap, $budget);
                }
            }
        }

        return $state;
    }

    public function isUninitializedLazy(object $object): bool
    {
        if ($object instanceof \Doctrine\Persistence\Proxy) {
            return !$object->__isInitialized();
        }
        if ($object instanceof \Symfony\Component\VarExporter\LazyObjectInterface) {
            return !$object->isLazyObjectInitialized();
        }
        if ($object instanceof \ProxyManager\Proxy\LazyLoadingInterface) {
            return !$object->isProxyInitialized();
        }
        if (\PHP_VERSION_ID >= 80400) {
            return (new \ReflectionClass($object))->isUninitializedLazyObject($object);
        }

        return false;
    }

    private function isOpaque(object $object): bool
    {
        $class = get_class($object);
        if ($this->isWorkerSafeClass($class)) {
            return true;
        }
        foreach ($this->opaqueNamespaces as $namespace) {
            if (str_starts_with($class, $namespace)) {
                return true;
            }
        }
        if ($object instanceof \ArrayObject || $object instanceof \ArrayIterator) {
            return false;
        }

        return (new \ReflectionClass($object))->isInternal();
    }

    public function isWorkerSafeClass(string $class): bool
    {
        if (!isset($this->workerSafeClasses[$class])) {
            $safe = false;
            for ($ref = new \ReflectionClass($class); $ref !== false; $ref = $ref->getParentClass()) {
                if ($ref->getAttributes(WorkerSafe::class) !== []) {
                    $safe = true;
                    break;
                }
            }
            $this->workerSafeClasses[$class] = $safe;
        }

        return $this->workerSafeClasses[$class];
    }

    /**
     * @return array<string, true> names of properties carrying #[WorkerSafe], including promoted ones
     */
    private function workerSafeProperties(string $class): array
    {
        if (!isset($this->workerSafeProps[$class])) {
            $names = [];
            for ($ref = new \ReflectionClass($class); $ref !== false; $ref = $ref->getParentClass()) {
                foreach ($ref->getProperties() as $prop) {
                    if ($prop->getAttributes(WorkerSafe::class) !== []) {
                        $names[$prop->getName()] = true;
                    }
                }
            }
            $this->workerSafeProps[$class] = $names;
        }

        return $this->workerSafeProps[$class];
    }
}
