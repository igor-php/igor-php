<?php
namespace App\Internal;

// Inlined definition: a private service injected only into InlinedConsumerService.
// Symfony inlines it into its parent singleton, where its state lives for the whole worker lifetime.
// Igor must keep auditing it even though it disappears from the container's definitions.
class InlinedRequestCounter {
    private int $count = 0;
    public function increment(): int { return ++$this->count; }
}
