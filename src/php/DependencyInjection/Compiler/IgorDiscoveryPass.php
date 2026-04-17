<?php

namespace KevinMartinsDev\IgorPhp\DependencyInjection\Compiler;

use Symfony\Component\DependencyInjection\Compiler\CompilerPassInterface;
use Symfony\Component\DependencyInjection\ContainerBuilder;

class IgorDiscoveryPass implements CompilerPassInterface
{
    public function process(ContainerBuilder $container): void
    {
        $serviceMap = [
            'definitions' => [],
            'aliases' => [],
        ];

        foreach ($container->getDefinitions() as $id => $definition) {
            if ($definition->isSynthetic() || !$definition->getClass()) {
                continue;
            }

            $serviceMap['definitions'][$id] = [
                'class' => $container->getParameterBag()->resolveValue($definition->getClass()),
                'public' => $definition->isPublic(),
                'shared' => $definition->isShared(),
            ];
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
}
