<?php

namespace App\Service;

class ArrayStateService
{
    private array $cache = [];
    private array $options = ['debug' => false];

    public function addData($key, $value): void
    {
        // Mutation via append (KO)
        $this->cache[] = $value;
        
        // Mutation via key (KO)
        $this->cache[$key] = $value;
    }

    public function updateOption($value): void
    {
        // Mutation via deep key (KO)
        $this->options['debug'] = $value;
    }

    public function clear(): void
    {
        // Empty value written in clear(): this write is the reset (OK, #105)
        $this->cache = [];
    }
}
