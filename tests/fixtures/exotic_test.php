<?php

// 1. Global keyword
global $database; // ERROR

// 2. Static in global function
function counter() {
    static $i = 0;
}

// 3. Mutation in anonymous class
$processor = new class {
    private $state;
    public function process($data) {
        $this->state = $data;
    }
};

// 4. Static mutation in global scope
self::$globalCache = "value";
