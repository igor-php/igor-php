<?php

namespace IgorPhp\IgorBundle\DependencyInjection\Compiler;

use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\DependencyInjection\Compiler\CompilerPassInterface;

/**
 * Records the container aliases before Symfony's removing passes run.
 *
 * RemovePrivateAliasesPass drops private aliases such as the autowiring alias
 * "App\FooInterface" => "App\Foo", which igor needs to resolve the services behind
 * interface type hints (e.g. to know that an injected interface is resettable).
 */
class IgorAliasSnapshotPass implements CompilerPassInterface
{
    private array $aliases = [];
    private array $targetClasses = [];

    public function process(ContainerBuilder $container): void
    {
        $this->aliases = [];
        $this->targetClasses = [];

        foreach ($container->getAliases() as $id => $alias) {
            $this->aliases[$id] = (string) $alias;

            // Remember the class behind the alias, in case its target gets inlined later on
            try {
                $class = $container->getParameterBag()->resolveValue($container->findDefinition($id)->getClass());
                if (is_string($class) && $class !== '') {
                    $this->targetClasses[$id] = $class;
                }
            } catch (\Throwable $e) {
                // Broken alias: keep the raw target only
            }
        }
    }

    /**
     * Returns the recorded aliases whose target is still part of $container.
     *
     * An alias whose target was inlined after the snapshot points to the target's class, provided
     * one of $liveClasses still backs it. Aliases to services pruned as unused are dropped.
     *
     * @param string[] $liveClasses classes of the definitions exported in the service map, inlined ones included
     */
    public function getAliases(ContainerBuilder $container, array $liveClasses): array
    {
        $liveClasses = array_flip($liveClasses);
        $aliases = [];
        foreach ($this->aliases as $id => $target) {
            if (!$container->hasDefinition($target) && !$container->hasAlias($target)) {
                if (!isset($this->targetClasses[$id], $liveClasses[$this->targetClasses[$id]])) {
                    continue;
                }
                $target = $this->targetClasses[$id];
            }
            $aliases[$id] = $target;
        }

        return $aliases;
    }
}
