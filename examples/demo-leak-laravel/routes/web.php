<?php

use App\Http\Controllers\LeakDemoController;
use Illuminate\Support\Facades\Route;

Route::controller(LeakDemoController::class)->group(function () {
    Route::get('/', 'index');
    Route::get('/stateful-service', 'stateful');
    Route::get('/incomplete-reset', 'incomplete');
    Route::get('/static-leak', 'staticLeak');
    Route::get('/heavy-load', 'heavyLoad');
    Route::get('/check-timezone', 'checkTimezone');
    Route::get('/poison-timezone', 'poisonTimezone');
    Route::get('/exit', 'exitDemo');
    Route::get('/doctrine-leak', 'doctrineLeak');
    Route::get('/poison-filters', 'poisonFilters');
    Route::get('/closure-leak', 'closureLeak');
    Route::get('/local-static', 'localStatic');
    Route::get('/superglobals', 'superglobals');
    Route::get('/destructor-leak', 'destructorLeak');
    Route::get('/process-state-leak', 'processStateLeak');
    Route::get('/stale-request', 'staleRequest');
    Route::get('/lazy-singleton', 'lazySingleton');
});
