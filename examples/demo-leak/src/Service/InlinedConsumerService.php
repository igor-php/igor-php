<?php
namespace App\Service;

use App\Internal\InlinedRequestCounter;

class InlinedConsumerService {
    public function __construct(private InlinedRequestCounter $counter) {}
    public function hit(): int { return $this->counter->increment(); }
}
