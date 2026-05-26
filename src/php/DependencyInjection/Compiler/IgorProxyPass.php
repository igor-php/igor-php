<?php

namespace IgorPhp\IgorBundle\DependencyInjection\Compiler;

use IgorPhp\IgorBundle\Service\IgorProxyFactory;
use IgorPhp\IgorBundle\Service\IgorUsageTracker;
use Symfony\Component\DependencyInjection\Compiler\CompilerPassInterface;
use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\DependencyInjection\Definition;
use Symfony\Component\DependencyInjection\Reference;

/**
 * Identifies leaky services and wraps them in a proxy to track runtime usage.
 */
class IgorProxyPass implements CompilerPassInterface
{
    public function process(ContainerBuilder $container): void
    {
        if (!$container->has(IgorUsageTracker::class)) {
            return;
        }

        // 1. Identify leaky classes (Simulation)
        $leakyClasses = [
            'App\Service\PassengerImplicitProfileChecker',
            'Tcs\Common\CoreBundle\Report\Logger\ReportLogger',
            'App\Service\IgorTestService',
        ];

        foreach ($container->getDefinitions() as $id => $definition) {
            if ($definition->isAbstract() || $definition->isSynthetic()) {
                continue;
            }

            $class = $definition->getClass();
            if (!$class) {
                continue;
            }

            $resolvedClass = $container->getParameterBag()->resolveValue($class);

            if (in_array($resolvedClass, $leakyClasses, true)) {
                $this->decorateLeakyService($id, $resolvedClass, $container);
            }
        }
    }

    private function decorateLeakyService(string $id, string $class, ContainerBuilder $container): void
    {
        $proxyId = $id . '.igor_usage_proxy';
        
        $container->register($proxyId, $class)
            ->setDecoratedService($id)
            ->setFactory([new Reference(IgorProxyFactory::class), 'createProxy'])
            ->setArguments([new Reference($proxyId . '.inner'), $class])
            ->setPublic(false);
    }
}
