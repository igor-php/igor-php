<?php

namespace IgorPhp\Laravel;

use Closure;
use Illuminate\Contracts\Foundation\Application;

/**
 * Builds the igor service map of a Laravel application, in the format written by the
 * Symfony bundle (definitions + aliases), so the igor binary audits both the same way.
 *
 * Under Octane, an object outlives the request only if it lives in the base application
 * that Octane clones for every request: the singletons resolved while the worker boots
 * (by the service providers, or listed in octane.warm). A singleton first resolved during
 * a request lives in the request's clone and is dropped with it, like bind() and scoped()
 * services and the bindings listed in octane.flush.
 */
final class ServiceMapBuilder
{
    /** Tag of the classes that run in the worker for every request without persisting (controllers). */
    public const ENTRYPOINT_TAG = 'igor.entrypoint';

    private const MAX_DEPTH = 10;

    /** Framework classes whose dependencies are left to Octane, which resets the framework state itself. */
    private const FRAMEWORK_NAMESPACES = ['Illuminate\\', 'Laravel\\'];

    /** Octane events fired around every request: a listener on any other event does not reset state between requests. */
    private const REQUEST_EVENTS = [
        'Laravel\\Octane\\Events\\RequestReceived',
        'Laravel\\Octane\\Events\\RequestHandled',
        'Laravel\\Octane\\Events\\RequestTerminated',
        'Laravel\\Octane\\Contracts\\OperationTerminated',
    ];

    private array $definitions = [];

    private array $warnings = [];

    /** @var array<string, true> Files of the closure routes, which run in the worker without any class */
    private array $entrypointFiles = [];

    /** @var array<string, string>|null Source code of the Octane listeners, by class */
    private ?array $listenerSources = null;

    public function __construct(private Application $app)
    {
    }

    public function build(): array
    {
        $this->definitions = [];
        $this->entrypointFiles = [];
        $this->warm();

        [$instances, $scoped, $aliases] = (function () {
            return [$this->instances, $this->scopedInstances, $this->aliases];
        })->call($this->app);

        // Bindings Octane drops before every request never outlive it
        $transient = array_fill_keys(array_merge($scoped, $this->config('octane.flush', [])), true);

        // 1. Objects of the base application: they persist across requests
        foreach ($instances as $abstract => $instance) {
            if (is_object($instance) && !isset($transient[$abstract])) {
                $this->addDefinition($abstract, get_class($instance), true);
            }
        }

        // 2. Other bindings: bind(), scoped(), flushed or lazy singletons, created per request
        foreach ($this->app->getBindings() as $abstract => $binding) {
            if (!isset($this->definitions[$abstract])) {
                $this->addDefinition($abstract, $this->concreteClass($abstract, $binding['concrete'] ?? null), false);
            }
        }

        // 3. Controllers are rebuilt for every request, but their code still runs in the worker
        $this->addControllers();

        // 4. What persistent services retain through their constructor persists with them
        foreach ($this->definitions as $id => $definition) {
            if ($definition['shared']) {
                $this->definitions[$id]['arguments'] = $this->dependencies($definition['class'], 0);
            }
        }

        return [
            'definitions' => $this->definitions,
            'aliases' => $aliases,
            'entrypoint_files' => array_keys($this->entrypointFiles),
            'warnings' => $this->warnings,
        ];
    }

    /** @return string[] */
    public function getWarnings(): array
    {
        return $this->warnings;
    }

    /** Resolves the octane.warm services like Octane does when a worker boots. */
    private function warm(): void
    {
        foreach ($this->config('octane.warm', []) as $service) {
            if (!is_string($service) || !$this->app->bound($service)) {
                continue;
            }
            try {
                $this->app->make($service);
            } catch (\Throwable $e) {
                $this->warnings[] = sprintf('Could not warm %s: %s', $service, $e->getMessage());
            }
        }
    }

    private function addDefinition(string $id, ?string $class, bool $shared, array $tags = []): void
    {
        if ($class === null || $class === Closure::class || str_contains($class, '@anonymous')) {
            return;
        }

        $this->definitions[$id] = [
            'class' => $class,
            'public' => true,
            'shared' => $shared,
            'resettable' => $shared && $this->isResettable($class),
            'arguments' => [],
            'tags' => $tags,
        ];
    }

    /** Finds the class a binding builds without resolving it, or null when only running its closure would tell. */
    private function concreteClass(string $abstract, mixed $concrete): ?string
    {
        if (is_string($concrete)) {
            return class_exists($concrete) ? $concrete : null;
        }
        if (!$concrete instanceof Closure) {
            return null;
        }

        $closure = new \ReflectionFunction($concrete);

        // bind(A::class, B::class) and singleton(A::class) wrap the class name in a closure
        $vars = $closure->getStaticVariables();
        if (isset($vars['concrete']) && is_string($vars['concrete'])) {
            return class_exists($vars['concrete']) ? $vars['concrete'] : null;
        }

        // A factory closure: trust its declared return type
        $type = $closure->getReturnType();
        if ($type instanceof \ReflectionNamedType && !$type->isBuiltin() && class_exists($type->getName())) {
            return $type->getName();
        }

        return class_exists($abstract) && !(new \ReflectionClass($abstract))->isAbstract() ? $abstract : null;
    }

    /** Builds a binding whose class only its factory knows, like the service that retained it did. */
    private function resolvedClass(string $abstract): ?string
    {
        try {
            $instance = $this->app->make($abstract);
        } catch (\Throwable $e) {
            $this->warnings[] = sprintf('Could not resolve %s to audit it: %s', $abstract, $e->getMessage());

            return null;
        }

        return is_object($instance) ? get_class($instance) : null;
    }

    private function addControllers(): void
    {
        if (!$this->app->bound('router')) {
            return;
        }

        foreach ($this->app['router']->getRoutes() as $route) {
            $uses = $route->getAction('uses');
            if ($uses instanceof Closure) {
                $file = (new \ReflectionFunction($uses))->getFileName();
                if ($file !== false) {
                    $this->entrypointFiles[$file] = true;
                }
                continue;
            }

            $action = $route->getAction('controller');
            if (!is_string($action)) {
                continue;
            }
            $class = explode('@', $action)[0];
            if (class_exists($class)) {
                $this->addDefinition('controller.' . $class, $class, false, [['name' => self::ENTRYPOINT_TAG]]);
            }
        }
    }

    /** Lists the services a class receives in its constructor, registering the ones it retains. */
    private function dependencies(string $class, int $depth): array
    {
        if ($depth >= self::MAX_DEPTH || $this->isFrameworkClass($class) || !class_exists($class)) {
            return [];
        }

        $constructor = (new \ReflectionClass($class))->getConstructor();
        if ($constructor === null) {
            return [];
        }

        $deps = [];
        foreach ($constructor->getParameters() as $parameter) {
            $type = $parameter->getType();
            if ($type instanceof \ReflectionNamedType && !$type->isBuiltin()) {
                $deps[] = ['type' => 'service', 'id' => $this->retainedDependency($type->getName(), $depth)];
            }
        }

        return $deps;
    }

    /** Returns the id of a dependency a persistent service retains, registering it as persistent if needed. */
    private function retainedDependency(string $type, int $depth): string
    {
        $abstract = $this->app->getAlias($type);
        if (isset($this->definitions[$abstract]) && $this->definitions[$abstract]['shared']) {
            return $abstract;
        }

        // Even a per-request binding is kept for the whole worker life by the service that received it
        $id = 'retained.' . $type;
        if (!isset($this->definitions[$id])) {
            $binding = $this->app->getBindings()[$abstract] ?? null;
            $class = $binding !== null
                ? ($this->concreteClass($abstract, $binding['concrete'] ?? null) ?? $this->resolvedClass($abstract))
                : (class_exists($type) ? $type : null);
            $this->addDefinition($id, $class, true);
            if (isset($this->definitions[$id])) {
                $this->definitions[$id]['arguments'] = $this->dependencies($this->definitions[$id]['class'], $depth + 1);
            }
        }

        return $id;
    }

    /**
     * A persistent service is resettable when it implements Symfony's ResetInterface, or when an
     * Octane listener resets it between requests: it has a reset() method and the code of a listener
     * of a request event references its class.
     */
    private function isResettable(string $class): bool
    {
        if (interface_exists('Symfony\Contracts\Service\ResetInterface') && is_subclass_of($class, 'Symfony\Contracts\Service\ResetInterface')) {
            return true;
        }
        if (!method_exists($class, 'reset')) {
            return false;
        }

        foreach ($this->listenerSources() as $source) {
            if (str_contains($source, $class)) {
                return true;
            }
        }

        return false;
    }

    /** @return array<string, string> */
    private function listenerSources(): array
    {
        if ($this->listenerSources !== null) {
            return $this->listenerSources;
        }

        $this->listenerSources = [];
        foreach ($this->config('octane.listeners', []) as $event => $listeners) {
            if (!in_array(ltrim((string) $event, '\\'), self::REQUEST_EVENTS, true)) {
                continue;
            }
            foreach ((array) $listeners as $listener) {
                if (!is_string($listener) || $this->isFrameworkClass($listener) || !class_exists($listener)) {
                    continue;
                }
                $file = (new \ReflectionClass($listener))->getFileName();
                if ($file !== false && is_readable($file)) {
                    $this->listenerSources[$listener] = (string) file_get_contents($file);
                }
            }
        }

        return $this->listenerSources;
    }

    private function isFrameworkClass(string $class): bool
    {
        foreach (self::FRAMEWORK_NAMESPACES as $namespace) {
            if (str_starts_with(ltrim($class, '\\'), $namespace)) {
                return true;
            }
        }

        return false;
    }

    private function config(string $key, array $default): array
    {
        $value = $this->app['config']->get($key, $default);

        return is_array($value) ? $value : $default;
    }
}
