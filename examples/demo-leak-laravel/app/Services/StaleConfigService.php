<?php

namespace App\Services;

use Illuminate\Contracts\Config\Repository;

class StaleConfigService
{
    // Captured once, when Octane warms this singleton: every request then works on its own copy
    public function __construct(private Repository $config) {}

    public function locale(): string
    {
        return (string) $this->config->get('lab.locale', '(unset)');
    }
}
