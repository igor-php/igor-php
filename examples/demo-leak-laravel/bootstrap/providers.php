<?php

use App\Providers\AppServiceProvider;
use IgorPhp\Laravel\IgorServiceProvider;

return [
    AppServiceProvider::class,
    // Exports the service map igor audits (php artisan igor:dump)
    IgorServiceProvider::class,
];
