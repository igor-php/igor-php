<?php

namespace App\Services;

use Illuminate\Contracts\Foundation\Application;

class StaleContainerService
{
    // Captured once, when Octane warms this singleton: the base application, not the request's clone
    public function __construct(private Application $app) {}

    public function describe(): array
    {
        return [
            'locale' => $this->app['config']->get('lab.locale', '(unset)'),
            'same_container' => $this->app === app(),
        ];
    }
}
