<?php

namespace App\Service;

use Symfony\Contracts\Service\ResetInterface;

class IncompleteService implements ResetInterface
{
    private $prop1;
    private $prop2;
    private $prop3;

    public function doWork($val): void
    {
        $this->prop1 = $val;
        $this->prop2 = $val;
        $this->prop3 = $val;
    }

    public function reset(): void
    {
        // Only resetting 2 out of 3 properties
        $this->prop1 = null;
        $this->prop2 = null;
        // Missing: $this->prop3 = null;
    }
}
