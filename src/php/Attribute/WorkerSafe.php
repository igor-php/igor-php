<?php

namespace IgorPhp\IgorBundle\Attribute;

use Attribute;

#[Attribute(Attribute::TARGET_CLASS | Attribute::TARGET_METHOD | Attribute::TARGET_PROPERTY | Attribute::TARGET_PARAMETER)]
class WorkerSafe
{
    public function __construct(
        public ?string $scope = null,
        public ?string $reason = null,
    ) {}
}
