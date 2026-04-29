<?php

use Illuminate\Http\Request;
use Illuminate\Support\Facades\Config;

define('LARAVEL_START', microtime(true));

require __DIR__.'/../vendor/autoload.php';

$app = require_once __DIR__.'/../bootstrap/app.php';

$kernel = $app->make(Illuminate\Contracts\Http\Kernel::class);

$handler = static function () use ($app, $kernel) {
    $request = Request::capture();
    $response = $kernel->handle($request);
    $response->send();
    $kernel->terminate($request, $response);

    // --- OCTANE SIMULATION: FLUSH SERVICES ---
    $toFlush = config('octane.flush', []);
    foreach ($toFlush as $service) {
        $app->forgetInstance($service);
    }
};

if (function_exists('frankenphp_handle_request')) {
    while (frankenphp_handle_request($handler));
} else {
    $handler();
}
