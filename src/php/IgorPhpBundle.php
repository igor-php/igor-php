<?php

namespace IgorPhp\IgorBundle;

use IgorPhp\IgorBundle\DependencyInjection\Compiler\IgorDiscoveryPass;
use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\HttpKernel\Bundle\Bundle;

class IgorPhpBundle extends Bundle
{
    public function build(ContainerBuilder $container): void
    {
        parent::build($container);

        $container->addCompilerPass(new IgorDiscoveryPass());
    }
}
