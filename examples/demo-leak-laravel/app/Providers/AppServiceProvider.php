<?php

namespace App\Providers;

use App\Services\ClosureLeakService;
use App\Services\DestructorLeakService;
use App\Services\FakeEntityManager;
use App\Services\IncompleteResetService;
use App\Services\LazySingletonService;
use App\Services\LocalStaticService;
use App\Services\ProcessStateLeakService;
use App\Services\StaleRequestService;
use App\Services\StatefulService;
use App\Services\StaticLeakService;
use Illuminate\Support\ServiceProvider;

class AppServiceProvider extends ServiceProvider
{
    /**
     * Register any application services.
     */
    public function register(): void
    {
        // Singletons warmed by Octane at worker boot (see 'warm' in config/octane.php):
        // they persist across every request served by the worker.
        $this->app->singleton(StatefulService::class);
        $this->app->singleton(IncompleteResetService::class);
        $this->app->singleton(StaticLeakService::class);
        $this->app->singleton(FakeEntityManager::class);
        $this->app->singleton(ClosureLeakService::class);
        $this->app->singleton(LocalStaticService::class);
        $this->app->singleton(DestructorLeakService::class);
        $this->app->singleton(ProcessStateLeakService::class);
        $this->app->singleton(StaleRequestService::class);

        // Singleton NOT warmed: Octane resolves it in the request sandbox and forgets it afterwards
        $this->app->singleton(LazySingletonService::class);
    }

    /**
     * Bootstrap any application services.
     */
    public function boot(): void
    {
        //
    }
}
