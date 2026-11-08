# Laravel (Octane) in FrankenPHP worker mode

## What persists

With Octane, the application boots once per worker. For each request Octane clones a **sandbox**
of the container, then runs the listeners of `config/octane.php`.

- **Persistent**: singletons resolved while the worker boots (by a service provider, or listed in
  `octane.warm`), and everything they keep (their dependencies).
- **Per request**: `bind()` and `scoped()` bindings, bindings listed in `octane.flush`, and
  singletons first resolved during a request (they live in the sandbox).
- **Without Octane** (a custom FrankenPHP worker loop reusing one application): every `singleton()`
  and `scoped()` persists once resolved. Only `bind()` is per request.

Igor learns this from `php artisan igor:dump` (registered by `IgorServiceProvider` through package
discovery), which writes `bootstrap/cache/igor_service_map.json`. If the app cannot boot from the
CLI, igor falls back to a directory scan and reports more false positives (DTOs, jobs, events...).

The `Illuminate\` and `Laravel\` namespaces are skipped: Octane resets the framework's own state.

## Fix toolbox

**Per-request service**: replace `singleton()` with `scoped()`, so the instance is rebuilt for
each request. The simplest fix when the service needs the request, the user or the config.

**Reset a persistent service** (Octane's `kernel.reset`): give it a `reset()` method and call it
from a listener on `RequestReceived` in `config/octane.php`:
```php
// app/Listeners/ResetCart.php
final class ResetCart
{
    public function handle($event): void
    {
        $event->sandbox->make(CartCalculator::class)->reset();
    }
}

// config/octane.php
RequestReceived::class => [
    ...Octane::prepareApplicationForNextOperation(),
    ...Octane::prepareApplicationForNextRequest(),
    \App\Listeners\ResetCart::class,
],
```
Implementing `Symfony\Contracts\Service\ResetInterface` alone does nothing in Laravel: nothing
calls it. Igor checks that `reset()` clears every property the service fills.

**Request, container, config**: never keep them in a singleton (constructor or property). Use
`request()`, `app()`, `config()` at call time, pass the request to the method, or inject a closure
`fn () => app('request')`.

**Facades and macros**: `Facade::$resolvedInstance` is cleared by Octane; your own statics
(`static $instances`, `Model::$something`) are not. Register macros and model boot logic once in a
service provider, never per request.

**`octane.flush`**: lists bindings Octane forgets before each request. Useful for a third-party
singleton you cannot change.

**Global helpers**: `config([...])` at runtime, `app()->setLocale()`, `URL::forceRootUrl()`,
`Carbon::setLocale()` change shared state. Octane restores some of them (locale, URL generator);
check `prepareApplicationForNextRequest()` before assuming.

## Boot-time code

Service providers' `register()`/`boot()`, Artisan commands and the Octane `warm` list run outside
requests: a finding there is only a leak if the stored value depends on a request.
