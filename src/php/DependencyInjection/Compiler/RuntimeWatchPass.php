<?php

namespace IgorPhp\IgorBundle\DependencyInjection\Compiler;

use IgorPhp\IgorBundle\Runtime\LeakWatcher;
use IgorPhp\IgorBundle\Runtime\ProcessStateProbe;
use IgorPhp\IgorBundle\Runtime\Report\JsonlReporter;
use IgorPhp\IgorBundle\Runtime\Report\LoggerReporter;
use IgorPhp\IgorBundle\Runtime\Report\TextReportFormatter;
use IgorPhp\IgorBundle\Runtime\RootCollector;
use IgorPhp\IgorBundle\Runtime\ServiceSnapshotter;
use IgorPhp\IgorBundle\Runtime\SnapshotDiffer;
use Symfony\Component\DependencyInjection\Compiler\CompilerPassInterface;
use Symfony\Component\DependencyInjection\ContainerBuilder;
use Symfony\Component\DependencyInjection\ContainerInterface;
use Symfony\Component\DependencyInjection\Reference;

/**
 * Registers the runtime leak watcher. Opt-in: nothing is registered unless the
 * "igor.runtime_watch.enabled" parameter is true.
 */
class RuntimeWatchPass implements CompilerPassInterface
{
    public const DEFAULT_IGNORED_NAMESPACES = ['Symfony\\', 'Doctrine\\', 'Psr\\', 'Twig\\', 'ApiPlatform\\', 'Monolog\\', 'IgorPhp\\IgorBundle\\'];

    // Above every framework listener, so the snapshot is taken before anything touches a service
    private const FIRST = 100000;
    private const LAST = -100000;

    public function process(ContainerBuilder $container): void
    {
        if ($this->parameter($container, 'enabled', false) !== true) {
            return;
        }

        $ignored = $this->parameter($container, 'ignore_namespaces', self::DEFAULT_IGNORED_NAMESPACES);

        $container->register('igor.runtime.snapshotter', ServiceSnapshotter::class)
            ->setArguments([
                $ignored,
                $this->parameter($container, 'max_depth', 12),
                $this->parameter($container, 'max_nodes', 5000),
            ]);

        $container->register('igor.runtime.root_collector', RootCollector::class)
            ->setArguments([new Reference('service_container'), new Reference('igor.runtime.snapshotter'), $ignored]);

        $container->register('igor.runtime.differ', SnapshotDiffer::class);
        $container->register('igor.runtime.process_state', ProcessStateProbe::class);
        $container->register('igor.runtime.text_formatter', TextReportFormatter::class);

        $container->register('igor.runtime.jsonl_reporter', JsonlReporter::class)
            ->setArguments([$this->parameter($container, 'jsonl_path', '%kernel.logs_dir%/igor-leaks.jsonl')]);

        $container->register('igor.runtime.logger_reporter', LoggerReporter::class)
            ->setArguments([
                new Reference('igor.runtime.text_formatter'),
                new Reference('logger', ContainerInterface::NULL_ON_INVALID_REFERENCE),
            ])
            ->addTag('monolog.logger', ['channel' => 'igor']);

        $container->register(LeakWatcher::class, LeakWatcher::class)
            ->setPublic(true)
            ->setArguments([
                new Reference('igor.runtime.root_collector'),
                new Reference('igor.runtime.snapshotter'),
                new Reference('igor.runtime.differ'),
                new Reference('igor.runtime.process_state'),
                [new Reference('igor.runtime.jsonl_reporter'), new Reference('igor.runtime.logger_reporter')],
                new Reference('services_resetter', ContainerInterface::NULL_ON_INVALID_REFERENCE),
                $this->parameter($container, 'phases', false),
            ])
            ->addTag('kernel.event_listener', ['event' => 'kernel.request', 'method' => 'onKernelRequest', 'priority' => self::FIRST])
            ->addTag('kernel.event_listener', ['event' => 'kernel.controller', 'method' => 'onKernelController', 'priority' => self::LAST])
            ->addTag('kernel.event_listener', ['event' => 'kernel.response', 'method' => 'onKernelResponse', 'priority' => self::LAST]);
    }

    private function parameter(ContainerBuilder $container, string $name, mixed $default): mixed
    {
        $name = 'igor.runtime_watch.' . $name;

        return $container->hasParameter($name) ? $container->getParameter($name) : $default;
    }
}
