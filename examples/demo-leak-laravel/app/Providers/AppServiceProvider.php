<?php

namespace App\Providers;

use Illuminate\Support\ServiceProvider;

class AppServiceProvider extends ServiceProvider
{
    /**
     * Register any application services.
     */
    public function register(): void
    {
        $this->app->singleton(\App\Services\StatefulService::class, \App\Services\StatefulService::class);
        $this->app->singleton(\App\Services\StaticLeakService::class, \App\Services\StaticLeakService::class);
        $this->app->singleton(\App\Services\ResettableService::class, \App\Services\ResettableService::class);
        $this->app->singleton(\App\Services\StaleRequestService::class, \App\Services\StaleRequestService::class);
        $this->app->singleton(\App\Services\StaleConfigService::class, \App\Services\StaleConfigService::class);
    }

    /**
     * Bootstrap any application services.
     */
    public function boot(): void
    {
        //
    }
}
