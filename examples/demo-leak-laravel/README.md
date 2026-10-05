# 🧟‍♂️ Igor-Php: Laravel Leak Lab (Octane + FrankenPHP)

The Laravel twin of the [Symfony Igor Leak Lab](../demo-leak/README.md). It demonstrates how PHP state "leaks" between requests when a Laravel application runs on **Laravel Octane** with **FrankenPHP workers**.

## 🧪 The Experiments

Start the project using Docker (only Docker is required: Composer runs inside the FrankenPHP image):
```bash
make start
```
This builds the image, runs `composer install` in a container and starts both services:

| Service | URL | Runtime |
|---|---|---|
| `php-worker` | [http://localhost:8090](http://localhost:8090) | `php artisan octane:frankenphp` (1 worker): Laravel boots once and serves every request |
| `php-classic` | [http://localhost:8091](http://localhost:8091) | Classic FrankenPHP: Laravel boots on every request |

The ports differ from the Symfony lab (8080/8081), so both labs can run side by side.

### 🔍 What to look for?
In this laboratory, **NOTHING** is stored in a database, session, cookie, or cache file (`SESSION_DRIVER=array`, `CACHE_STORE=array`).
Everything you see is stored **exclusively in PHP's RAM**.

1.  **Stateful Service Leak**: Property mutation in a warmed singleton.
2.  **Incomplete Reset Leak**: An Octane `RequestReceived` listener resets the service but forgets a property.
3.  **Static Property Leak**: How static data survives everything (Class level).
4.  **Global State Poisoning**: Changing process settings (Timezone).
5.  **Memory Pressure**: See how RAM usage grows and STAYS high.
6.  **The Danger of Exit/Die**: Killing the worker thread.
7.  **Shared Service Indirect Mutation**: Disabling filters on a shared manager fetched into a local variable.
8.  **Closure State Leak**: Leaking state or request data via unmanaged closures.
9.  **Local Static Variable**: Retaining state inside method execution using static variables.
10. **PHP Superglobals**: Direct pollution of `$_ENV`, reading `$_GET`.
11. **Magic Method __destruct() Bypass**: `__destruct()` never runs in persistent workers until worker exit.
12. **Dangerous Process State Mutations**: Functions altering process-wide C/PHP state (`chdir`, `umask`, `mb_internal_encoding`, `gc_disable`).
13. **Stale Request Captured by a Singleton** *(Laravel)*: a warmed singleton receives `Request` in its constructor and keeps the boot-time request forever.
14. **Lazy Singleton** *(Laravel, no leak)*: the same code as experiment 1, but not warmed: Octane throws it away after every request.

## 🧠 How Octane keeps state

Laravel's container works differently from Symfony's, and these differences decide what leaks:

- At worker boot, Octane bootstraps the application, loads deferred providers and resolves every binding listed in `octane.warm` ([`config/octane.php`](config/octane.php)).
- For **each request**, Octane serves a `clone` of that base application, then flushes the clone (`Laravel\Octane\Worker::handle()`).
- So only objects that already exist in the **base application** survive between requests: warmed singletons (experiments 1, 2, 7, 8, 11, 13) and everything they reference. A singleton first resolved during a request lives in the clone and disappears with it (experiment 14).
- **Static properties, local `static` variables and process-wide settings ignore the container entirely**: they leak in any class (experiments 3, 4, 9, 10, 12).
- `scoped()` bindings, `#[Scoped]` classes, `octane.flush` entries and Octane listeners are the Laravel equivalents of Symfony's `ResetInterface` / `kernel.reset` (experiment 2).
- Controllers are **not** persistent: Octane calls `Route::flushController()` after every request.

Where the lab registers its services:

| File | Role |
|---|---|
| [`app/Providers/AppServiceProvider.php`](app/Providers/AppServiceProvider.php) | Registers the lab services with `$this->app->singleton()` |
| [`config/octane.php`](config/octane.php) | `warm`: services resolved at worker boot (persistent); `listeners`: the incomplete reset of experiment 2 |
| [`app/Listeners/ResetIncompleteResetService.php`](app/Listeners/ResetIncompleteResetService.php) | Octane `RequestReceived` listener that resets only part of `IncompleteResetService` |

## 🛡️ How Igor-Php helps

Run Igor on this project:

```bash
igor-php examples/demo-leak-laravel
```

Igor has no Laravel bridge yet: without a Symfony `bin/console`, it falls back to a standard directory scan. This lab is the reference to build and test Laravel support. What the scan reports today:

| # | Experiment | Igor today |
|---|---|---|
| 1, 3, 4, 6, 7, 8, 9, 10, 11, 12 | Generic leaks | ✅ Detected |
| 2 | Incomplete reset via an Octane listener | ⚠️ Both properties reported: Igor does not know the Octane listener resets `$clean` |
| 13 | Stale `Request` captured by a warmed singleton | ❌ Not detected (needs a Laravel-specific rule) |
| 14 | Lazy (non-warmed) singleton | ❌ False positive: it is reset by the Octane sandbox |

Hints still mention Symfony (`Use Symfony response…`); they should become framework-aware.

## 🧹 Cleanup
```bash
make stop
```
