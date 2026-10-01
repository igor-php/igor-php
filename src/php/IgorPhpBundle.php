<?php

namespace IgorPhp\IgorBundle;

use IgorPhp\IgorBundle\DependencyInjection\Compiler\IgorDiscoveryPass;
use Symfony\Component\DependencyInjection\Compiler\PassConfig;
use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\HttpKernel\Bundle\Bundle;

class IgorPhpBundle extends Bundle
{
    public function build(ContainerBuilder $container): void
    {
        parent::build($container);

        // Run after Symfony removed unused definitions, and last within that stage so that
        // definitions registered by other after-removing passes are captured too.
        $container->addCompilerPass(new IgorDiscoveryPass(), PassConfig::TYPE_AFTER_REMOVING, -1000);
    }
}
