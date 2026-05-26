<?php

namespace IgorPhp\IgorBundle;

use IgorPhp\IgorBundle\DependencyInjection\Compiler\IgorDiscoveryPass;
use IgorPhp\IgorBundle\DependencyInjection\Compiler\IgorProxyPass;
use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\HttpKernel\Bundle\Bundle;

class IgorPhpBundle extends Bundle
{
    public function build(ContainerBuilder $container): void
    {
        parent::build($container);

        $container->addCompilerPass(new IgorDiscoveryPass());
        $container->addCompilerPass(new IgorProxyPass());
    }
}
