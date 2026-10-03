<?php

namespace IgorPhp\IgorBundle\Runtime;

/**
 * The findings attributed to one request (or to the time between two requests).
 */
class LeakReport
{
    /**
     * @param Finding[] $findings
     * @param array{method?: string, path?: string, route?: ?string, payload_shape?: array, label?: ?string} $context
     */
    public function __construct(
        public readonly array $findings,
        public readonly array $context,
        public readonly int $requestNumber,
    ) {
    }

    public function toArray(): array
    {
        return [
            'ts' => date(\DATE_ATOM),
            'request_number' => $this->requestNumber,
            'method' => $this->context['method'] ?? null,
            'path' => $this->context['path'] ?? null,
            'route' => $this->context['route'] ?? null,
            'label' => $this->context['label'] ?? null,
            'payload_shape' => $this->context['payload_shape'] ?? [],
            'findings' => array_map(static fn (Finding $f) => $f->toArray(), $this->findings),
        ];
    }
}
