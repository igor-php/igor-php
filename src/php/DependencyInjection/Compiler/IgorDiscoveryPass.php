<?php

namespace IgorPhp\IgorBundle\DependencyInjection\Compiler;

use Symfony\Component\DependencyInjection\Compiler\CompilerPassInterface;
use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\DependencyInjection\Definition;
use Symfony\Component\DependencyInjection\Reference;

class IgorDiscoveryPass implements CompilerPassInterface
{
    public function process(ContainerBuilder $container): void
    {
        $serviceMap = [
            'definitions' => [],
            'aliases' => [],
        ];

        foreach ($container->getDefinitions() as $id => $definition) {
            $this->registerDefinition($id, $definition, $container, $serviceMap, $definition->isShared());
            $this->collectInlinedDefinitions($definition, $container, $serviceMap, $definition->isShared());
        }

        foreach ($container->getAliases() as $id => $alias) {
            $serviceMap['aliases'][$id] = (string) $alias;
        }

        $cacheDir = $container->getParameter('kernel.cache_dir');
        if (!is_dir($cacheDir)) {
            mkdir($cacheDir, 0777, true);
        }

        file_put_contents(
            $cacheDir . '/igor_service_map.json',
            json_encode($serviceMap, JSON_PRETTY_PRINT)
        );
    }

    private function registerDefinition(string $id, Definition $definition, ContainerBuilder $container, array &$serviceMap, bool $isShared): void
    {
        if (isset($serviceMap['definitions'][$id])) {
            return;
        }

        if ($definition->isSynthetic() || !$definition->getClass() || $definition->hasTag('container.excluded')) {
            return;
        }

        $class = $container->getParameterBag()->resolveValue($definition->getClass());
        if (!$class || !is_string($class)) {
            return;
        }

        $isResettable = $definition->hasTag('kernel.reset');
        if (!$isResettable) {
            try {
                if (class_exists($class)) {
                    $isResettable = is_subclass_of($class, 'Symfony\Contracts\Service\ResetInterface');
                }
            } catch (\Throwable $e) {
                $isResettable = false;
            }
        }

        $serviceMap['definitions'][$id] = [
            'class' => $class,
            'public' => $definition->isPublic(),
            'shared' => $isShared,
            'resettable' => $isResettable,
            'arguments' => $this->extractDependencies($definition, $container),
        ];
    }

    private function collectInlinedDefinitions(Definition $definition, ContainerBuilder $container, array &$serviceMap, bool $parentShared): void
    {
        $values = [];
        $values[] = $definition->getArguments();

        foreach ($definition->getMethodCalls() as $call) {
            if (isset($call[1]) && is_array($call[1])) {
                $values[] = $call[1];
            }
        }

        $values[] = $definition->getProperties();

        $configurator = $definition->getConfigurator();
        if (is_array($configurator)) {
            $values[] = $configurator;
        }

        $factory = $definition->getFactory();
        if (is_array($factory)) {
            $values[] = $factory;
        }

        $this->extractDefinitionsRecursively($values, $container, $serviceMap, $parentShared);
    }

    private function extractDefinitionsRecursively($data, ContainerBuilder $container, array &$serviceMap, bool $parentShared): void
    {
        if ($data instanceof Definition) {
            $rawClass = $data->getClass();
            if ($rawClass) {
                $resolvedClass = $container->getParameterBag()->resolveValue($rawClass);
                if (is_string($resolvedClass) && $resolvedClass !== '') {
                    $inlinedId = 'inlined.' . $resolvedClass . '.' . spl_object_id($data);
                    $this->registerDefinition($inlinedId, $data, $container, $serviceMap, $parentShared);
                    $this->collectInlinedDefinitions($data, $container, $serviceMap, $parentShared);
                }
            }
            return;
        }

        if (is_array($data) || $data instanceof \Traversable) {
            foreach ($data as $item) {
                $this->extractDefinitionsRecursively($item, $container, $serviceMap, $parentShared);
            }
        }
    }

    private function extractDependencies(Definition $definition, ContainerBuilder $container): array
    {
        $deps = [];

        $extract = function ($val) use (&$deps, &$extract, $container) {
            if ($val instanceof Reference) {
                $deps[] = ['type' => 'service', 'id' => (string) $val];
            } elseif ($val instanceof Definition && $val->getClass()) {
                $rawClass = $val->getClass();
                $resolvedClass = $container->getParameterBag()->resolveValue($rawClass);
                if (is_string($resolvedClass) && $resolvedClass !== '') {
                    $deps[] = ['type' => 'service', 'id' => 'inlined.' . $resolvedClass . '.' . spl_object_id($val)];
                }
            } elseif (is_array($val) || $val instanceof \Traversable) {
                foreach ($val as $subVal) {
                    $extract($subVal);
                }
            }
        };

        foreach ($definition->getArguments() as $arg) {
            $extract($arg);
        }

        foreach ($definition->getMethodCalls() as $call) {
            if (isset($call[1]) && is_array($call[1])) {
                foreach ($call[1] as $arg) {
                    $extract($arg);
                }
            }
        }

        return $deps;
    }
}
