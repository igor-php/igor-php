<?php

namespace App\Service;

class StatelessService
{
    private readonly string $name;

    public function __construct(string $name)
    {
        $this->name = $name;
    }

    public function getName(): string
    {
        return $this->name;
    }

    public function compute(int $a, int $b): int
    {
        $localResult = $a + $b;
        return $localResult;
    }
}
