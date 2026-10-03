<?php

namespace IgorPhp\IgorBundle\Runtime;

/**
 * One piece of state that was different after a request than before it.
 */
class Finding
{
    /**
     * @param string[] $added summaries of the entries added to a growing array
     */
    public function __construct(
        public readonly string $serviceId,
        public readonly string $class,
        public readonly string $path,
        public readonly string $kind,
        public readonly string $before,
        public readonly string $after,
        public readonly int $delta = 0,
        public readonly array $added = [],
        public readonly ?string $phase = null,
        public readonly int $occurrences = 1,
        public readonly int $requestsObserved = 1,
    ) {
    }

    public function toArray(): array
    {
        return [
            'service' => $this->serviceId,
            'class' => $this->class,
            'path' => $this->path,
            'kind' => $this->kind,
            'before' => $this->before,
            'after' => $this->after,
            'delta' => $this->delta,
            'added' => $this->added,
            'phase' => $this->phase,
            'occurrences' => $this->occurrences,
            'requests_observed' => $this->requestsObserved,
        ];
    }
}
