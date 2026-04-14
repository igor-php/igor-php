<?php

namespace App\Service;

readonly class SafeReadonlyService {
    public function __construct(
        public string $name,
    ) {}

    public function doSomething() {
        // This is safe because the class is readonly
        // (Even if we tried to mutate, PHP would stop us)
        $this->name = "new name"; 
    }
}

class MixedService {
    public readonly string $id;
    private int $counter = 0;

    public function __construct(
        public readonly string $promotedReadonly,
    ) {}

    public function update($val) {
        $this->counter = $val; // Should be ERROR
        $this->promotedReadonly = "new"; // Should be IGNORED
    }
    
    public function setId($val) {
        // Technically this is a mutation, but since it's readonly, 
        // it can only happen once.
        $this->id = $val; 
    }

    public function hiddenMutation($data) {
        $this->cache->set('last', $data); // DANGEROUS: Interior mutability
        $this->items->add($data);         // DANGEROUS
    }
}
