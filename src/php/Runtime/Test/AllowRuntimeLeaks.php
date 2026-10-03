<?php

namespace IgorPhp\IgorBundle\Runtime\Test;

use Attribute;

/**
 * Opts a test class or method out of the automatic check of AutoRuntimeLeakCheckTrait.
 */
#[Attribute(Attribute::TARGET_CLASS | Attribute::TARGET_METHOD)]
class AllowRuntimeLeaks
{
    public function __construct(public ?string $reason = null)
    {
    }
}
