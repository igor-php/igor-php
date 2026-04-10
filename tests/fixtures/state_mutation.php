<?php

namespace App\Service;

class StatefulService
{
    private $count = 0;
    private static $cache = [];

    public function __construct()
    {
        $this->count = 1;
    }

    public function increment()
    {
        $this->count++; // ERROR: PostInc
        ++$this->count; // ERROR: PreInc
    }

    public function update(int $val)
    {
        $this->count = $val;
        self::$cache[] = $val;
    }

    public function complex()
    {
        [$this->count, $other] = [10, 20];
        $this->count ??= 5;
    }
}
