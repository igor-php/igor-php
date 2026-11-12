<?php

namespace App\Service;

// Remembers the last visitor through a destructuring: [$this->a, $this->b] = ... writes the
// properties exactly like $this->a = ...; $this->b = ...; and they outlive the request the same way.
class DestructuringLeakService
{
    private ?string $lastVisitor = null;
    private int $visits = 0;

    /**
     * Records a visit and returns the visitor recorded before it.
     */
    public function record(string $visitor): ?string
    {
        $previous = $this->lastVisitor;
        [$this->lastVisitor, $this->visits] = [$visitor, $this->visits + 1];

        return $previous;
    }

    public function getVisits(): int
    {
        return $this->visits;
    }
}
