<?php

namespace IgorPhp\IgorBundle;

use IgorPhp\IgorBundle\DependencyInjection\Compiler\IgorAliasSnapshotPass;
use IgorPhp\IgorBundle\DependencyInjection\Compiler\IgorDiscoveryPass;
use Symfony\Component\DependencyInjection\Compiler\PassConfig;
use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\HttpKernel\Bundle\Bundle;

class IgorPhpBundle extends Bundle
{
    public function build(ContainerBuilder $container): void
    {
        parent::build($container);

        // Snapshot the aliases before RemovePrivateAliasesPass drops the private ones
        // (e.g. interface autowiring aliases), which igor needs to resolve interface type hints.
        $aliasSnapshot = new IgorAliasSnapshotPass();
        $container->addCompilerPass($aliasSnapshot, PassConfig::TYPE_BEFORE_REMOVING, -1000);

        // Run after Symfony removed unused definitions, and last within that stage so that
        // definitions registered by other after-removing passes are captured too.
        $container->addCompilerPass(new IgorDiscoveryPass($aliasSnapshot), PassConfig::TYPE_AFTER_REMOVING, -1000);
    }
}
