<?php

use App\Http\Controllers\LeakDemoController;
use Illuminate\Support\Facades\Route;

Route::get('/', [LeakDemoController::class, 'index']);
Route::get('/stateful-service', [LeakDemoController::class, 'stateful']);
Route::get('/resettable-service', [LeakDemoController::class, 'resettable']);
Route::get('/stale-request', [LeakDemoController::class, 'staleRequest']);
Route::get('/stale-config', [LeakDemoController::class, 'staleConfig']);
Route::get('/static-leak', [LeakDemoController::class, 'staticLeak']);
Route::get('/check-timezone', [LeakDemoController::class, 'checkTimezone']);
Route::get('/poison-timezone', [LeakDemoController::class, 'poisonTimezone']);
Route::get('/heavy-load', [LeakDemoController::class, 'heavyLoad']);
Route::get('/exit', [LeakDemoController::class, 'exitDemo']);
