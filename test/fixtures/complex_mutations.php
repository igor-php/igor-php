<?php

namespace App\Service;

class ComplexStateService
{
    private $data = [];
    private $counter = 0;

    public function nested(): void
    {
        // Nested mutation (KO)
        $this->data['user']['profile']->name = 'Kevin';
    }

    public function dynamic($propName): void
    {
        // Dynamic property access (KO)
        $this->{$propName} = 'value';
    }

    public function reference(): void
    {
        // Passed by reference (KO)
        $this->doMutation($this->counter);
    }

    private function doMutation(&$val): void
    {
        $val++;
    }
}
