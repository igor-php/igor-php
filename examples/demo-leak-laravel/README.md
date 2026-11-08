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
In this laboratory, no experiment state is stored in a database, session, cookie, or cache file (`SESSION_DRIVER=array`, `CACHE_STORE=array`).
The leak demonstrations use **PHP's RAM only**, except experiment 11, which writes a disk-backed log (`storage/destructor_demo_<mode>.log`, one file per runtime mode) to show when constructors and destructors run.

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
15. **Stale Container Captured by a Singleton** *(Laravel)*: a warmed singleton receives the `Application` and keeps the base application, never the request's clone (`?locale=` set during the request is invisible to it).
16. **Stale Config Repository Captured by a Singleton** *(Laravel)*: a warmed singleton receives the `config` repository; Octane gives every request its own copy, so `config([...])` set during the request never reaches it.
17. **Static Array Written from a Controller**: a controller appends to `AuditTrail::$entries`, a static array no container owns, which grows for the whole worker life.

## 🧠 How Octane keeps state

Laravel's container works differently from Symfony's, and these differences decide what leaks:

- At worker boot, Octane bootstraps the application, loads deferred providers and resolves every binding listed in `octane.warm` ([`config/octane.php`](config/octane.php)).
- For **each request**, Octane serves a `clone` of that base application, then flushes the clone (`Laravel\Octane\Worker::handle()`).
- So only objects that already exist in the **base application** survive between requests: warmed singletons (experiments 1, 2, 7, 8, 11, 13, 15, 16) and everything they reference. A singleton first resolved during a request lives in the clone and disappears with it (experiment 14).
- **Static properties, local `static` variables and process-wide settings ignore the container entirely**: they leak in any class, a controller included (experiments 3, 4, 9, 10, 12, 17).
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

Igor detects `artisan` and runs `php artisan igor:dump` (registered by `IgorPhp\Laravel\IgorServiceProvider` in [`bootstrap/providers.php`](bootstrap/providers.php)) to learn which services persist in the Octane worker. It then audits those services, their dependencies and the controllers:

| # | Experiment | Igor |
|---|---|---|
| 1, 3, 4, 6, 7, 8, 9, 10, 11, 12, 17 | Generic leaks | ✅ Detected (17: the static write is reported in the controller) |
| 2 | Incomplete reset via an Octane listener | ✅ Only `$forgotten` reported: Igor knows the listener calls `reset()` |
| 13 | Stale `Request` captured by a warmed singleton | ✅ Detected: the singleton keeps the `Request` of the first request |
| 14 | Lazy (non-warmed) singleton | ✅ Not reported: it lives in the request's sandbox |
| 15 | Stale `Application` captured by a warmed singleton | ✅ Detected: the singleton keeps the base application |
| 16 | Stale `config` repository captured by a warmed singleton | ✅ Detected: the singleton keeps the boot-time configuration |

Hints use Laravel wording when Igor detects a Laravel application (`abort()`, `session()`, Octane listeners…).

### 🤖 With Claude Code or another agent

The lab is also a good playground for the [Claude Code plugin](../../plugins/igor/README.md) and the [MCP server](../../README.md#-mcp-server). With the plugin installed, open the lab in Claude Code and ask *"audit this project for FrankenPHP worker mode"*: Claude sees `artisan` and `laravel/octane`, runs the audit, and proposes the Laravel fixes (`scoped()`, a `reset()` called from an Octane listener, resolving the request at call time).

Any MCP client can drive the same audit:

```bash
igor-php mcp examples/demo-leak-laravel
```

- `project_info` reports Laravel with Octane and whether the Deep Audit runs.
- `audit_project` returns the findings of the table above, with their `rule_id`.
- `explain_service` on `App\Services\StatefulService` shows that it is shared, its dependencies, its findings and its verdict (it persists because `config/octane.php` warms it). On a service Igor does not audit, it says why it is skipped.
- `audit_file` checks a fix before it is written, from the unsaved content of the file.

## 🧹 Cleanup
```bash
make stop
```
