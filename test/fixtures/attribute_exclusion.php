<?php

namespace App\Service;

use Igor\WorkerSafe;

#[\Igor\WorkerSafe(scope: 'boot-time', reason: 'Entire class is safe')]
class EntirelySafeService {
    private $cache = [];
    public function set($v) {
        $this->cache[] = $v; // Should NOT be reported (Class is safe)
    }
}

class MixedService {
    private $normalProp = [];
    
    #[WorkerSafe]
    private $safeProp = [];

    #[WorkerSafe]
    public function setSafe($v) {
        $this->normalProp[] = $v; // Should NOT be reported (Method is safe)
    }

    public function setNormal($v) {
        $this->normalProp[] = $v; // SHOULD be reported (unsafe)
        $this->safeProp[] = $v;   // Should NOT be reported (Property is safe)
    }
}

class ResetSafeService implements \Symfony\Contracts\Service\ResetInterface {
    #[WorkerSafe]
    private $safeProp = [];

    private $unsafeProp = [];

    public function set($v) {
        $this->safeProp[] = $v;   // Should NOT be reported (Property is safe)
        $this->unsafeProp[] = $v; // Should be reported unless reset
    }

    public function reset() {
        // unsafeProp is not reset, which should trigger a WARNING.
        // safeProp is not reset, which should NOT trigger a WARNING.
    }
}

class PromotedSafeService {
    public function __construct(
        #[WorkerSafe]
        private $safeProp,
        private $unsafeProp,
    ) {}

    public function mutate() {
        $this->safeProp = "new";   // Should NOT be reported
        $this->unsafeProp = "new"; // Should be reported (unsafe)
    }
}
