<?php

namespace IgorPhp\Laravel;

use IgorPhp\Laravel\Console\DumpServiceMapCommand;
use Illuminate\Support\ServiceProvider;

class IgorServiceProvider extends ServiceProvider
{
    public function boot(): void
    {
        if ($this->app->runningInConsole()) {
            $this->commands([DumpServiceMapCommand::class]);
        }
    }
}
