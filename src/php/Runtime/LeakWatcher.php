<?php

namespace IgorPhp\IgorBundle\Runtime;

use IgorPhp\IgorBundle\Attribute\WorkerSafe;
use IgorPhp\IgorBundle\Runtime\Report\LeakReporterInterface;
use Symfony\Component\HttpFoundation\Request;
use Symfony\Component\HttpKernel\Event\KernelEvent;

/**
 * Snapshots every live shared service at the start of each request (after Symfony has reset the
 * resettable ones) and reports what differs from the previous snapshot.
 */
#[WorkerSafe(scope: 'diagnostic', reason: 'Keeps snapshots between requests on purpose; only registered when igor.runtime_watch.enabled is true')]
class LeakWatcher
{
    public const PHASE_BEFORE_CONTROLLER = 'before controller';
    public const PHASE_CONTROLLER = 'controller';
    public const PHASE_AFTER_RESPONSE = 'after response';

    /** @var array<string, array{class: string, tree: mixed}>|null */
    private ?array $previous = null;
    /** @var array<string, mixed> request context of the cycle being observed */
    private array $context = [];
    private ?string $label = null;
    private int $requestNumber = 0;
    /** @var array<string, int> number of observed request cycles per service */
    private array $cycles = [];
    /** @var array<string, int> number of cycles in which a path changed */
    private array $occurrences = [];
    /** @var array<string, string> phase at which a path was first seen changed in the current cycle */
    private array $phases = [];
    /** @var LeakReport[] */
    private array $reports = [];

    /**
     * @param iterable<LeakReporterInterface> $reporters
     */
    public function __construct(
        private RootCollector $roots,
        private ServiceSnapshotter $snapshotter,
        private SnapshotDiffer $differ,
        private ProcessStateProbe $processState,
        private iterable $reporters = [],
        private ?object $servicesResetter = null,
        private bool $trackPhases = false,
    ) {
    }

    public function onKernelRequest(KernelEvent $event): void
    {
        if (!$event->isMainRequest()) {
            return;
        }

        // Kernel::boot() has just run services_resetter: what still differs now was not cleaned up
        $this->closeCycle();

        ++$this->requestNumber;
        $this->context = $this->describe($event->getRequest());
    }

    public function onKernelController(KernelEvent $event): void
    {
        if ($event->isMainRequest() && $this->trackPhases) {
            $this->markPhase(self::PHASE_BEFORE_CONTROLLER);
        }
    }

    public function onKernelResponse(KernelEvent $event): void
    {
        if (!$event->isMainRequest()) {
            return;
        }

        // The route is only known once the router listener has run
        $this->context = $this->describe($event->getRequest());
        if ($this->trackPhases) {
            $this->markPhase(self::PHASE_CONTROLLER);
        }
    }

    /**
     * Closes the current request cycle without waiting for the next request: resets the resettable
     * services like the kernel would, then compares.
     */
    public function flush(): void
    {
        if ($this->servicesResetter !== null && method_exists($this->servicesResetter, 'reset')) {
            $this->servicesResetter->reset();
        }
        $this->closeCycle();
        $this->context = [];
    }

    public function setLabel(?string $label): void
    {
        $this->label = $label;
    }

    /**
     * @return LeakReport[]
     */
    public function getReports(): array
    {
        return $this->reports;
    }

    public function clearReports(): void
    {
        $this->reports = [];
    }

    private function closeCycle(): void
    {
        $current = $this->takeSnapshot();
        $findings = [];

        foreach ($current as $id => $entry) {
            if (!isset($this->previous[$id])) {
                // First sighting: this snapshot is the baseline, there is nothing to compare it with
                continue;
            }

            $this->cycles[$id] = ($this->cycles[$id] ?? 0) + 1;

            foreach ($this->differ->diff($this->previous[$id]['tree'], $entry['tree']) as $change) {
                $key = $id . '|' . $change['path'];
                $this->occurrences[$key] = ($this->occurrences[$key] ?? 0) + 1;

                $findings[] = new Finding(
                    $id,
                    $entry['class'],
                    $change['path'],
                    $change['kind'],
                    $change['before'],
                    $change['after'],
                    $change['delta'],
                    $change['added'],
                    $this->trackPhases ? ($this->phases[$key] ?? self::PHASE_AFTER_RESPONSE) : null,
                    $this->occurrences[$key],
                    $this->cycles[$id],
                );
            }
        }

        $this->previous = $current;
        $this->phases = [];

        if ($findings === []) {
            return;
        }

        $report = new LeakReport($findings, $this->context, $this->requestNumber);
        $this->reports[] = $report;
        foreach ($this->reporters as $reporter) {
            $reporter->report($report);
        }
    }

    /**
     * Records the phase in which each changed path is first noticed. Changes seen here may still be
     * cleaned up by a reset later, so they only annotate findings, they never create one.
     */
    private function markPhase(string $phase): void
    {
        if ($this->previous === null) {
            return;
        }

        foreach ($this->takeSnapshot() as $id => $entry) {
            if (!isset($this->previous[$id])) {
                continue;
            }
            foreach ($this->differ->diff($this->previous[$id]['tree'], $entry['tree']) as $change) {
                $this->phases[$id . '|' . $change['path']] ??= $phase;
            }
        }
    }

    /**
     * @return array<string, array{class: string, tree: mixed}>
     */
    private function takeSnapshot(): array
    {
        [$roots, $serviceMap] = $this->roots->collect();

        $snapshot = [];
        foreach ($roots as $id => $root) {
            $snapshot[$id] = ['class' => get_class($root), 'tree' => $this->snapshotter->snapshot($root, $serviceMap)];
        }

        $process = $this->snapshotter->snapshotValue($this->processState->read());
        $snapshot[ProcessStateProbe::ROOT_ID] = [
            'class' => ProcessStateProbe::ROOT_CLASS,
            'tree' => ['@t' => 'object', 'class' => ProcessStateProbe::ROOT_CLASS, 'props' => $process['items']],
        ];

        return $snapshot;
    }

    private function describe(Request $request): array
    {
        return [
            'method' => $request->getMethod(),
            'path' => $request->getPathInfo(),
            'route' => $request->attributes->get('_route'),
            'payload_shape' => [
                'query' => $this->shape($request->query->all()),
                'body' => $this->shape($request->request->all()),
            ],
            'label' => $this->label,
        ];
    }

    /**
     * Keys and value types only: request values never reach a report.
     */
    private function shape(array $values): array
    {
        $shape = [];
        foreach ($values as $key => $value) {
            $shape[$key] = is_array($value) ? $this->shape($value) : get_debug_type($value);
        }

        return $shape;
    }
}
