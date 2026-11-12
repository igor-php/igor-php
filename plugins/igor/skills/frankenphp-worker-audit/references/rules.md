# igor-php rules: what each finding means and how to fix it

Match a finding on its `rule_id` (the section title; `igor-php rule <rule_id>` prints the same
section), or on its `message` with versions of igor that have no `rule_id`. Each rule gives why it leaks in worker mode, the fix, and when
the finding is a false positive. Framework details: `symfony.md`, `laravel-octane.md`.

## StateMutation

**Messages**: `Mutation of state '<prop>' in <Class>::<method>()`,
`Mutation of static state '<Class>::$<prop>' in ...`, with `category: cache` when the write is
keyed (`$this->cache[$key] = ...`, see `cache_key`).

**Why**: a shared service writes one of its properties (or a static) while serving a request. The
value stays for the next requests. A write through a destructuring (`[$this->a, $this->b] = $row`,
`list(...)`, keyed and nested forms) or a `foreach` target (`foreach ($rows as $this->current)`)
counts the same as a plain assignment.

**Fix**, from best to acceptable:
1. Make the value local: return it, or pass it as a method argument instead of storing it.
2. Read request data when needed (current request, security token, locale) instead of storing it.
3. Per-request state on purpose: reset it between requests (Symfony `ResetInterface` +
   `kernel.reset`; Laravel `scoped()` under Octane, or a `reset()` called from an Octane listener).
4. Cache (`category: cache`): keep it only if the key space is bounded and does not depend on the
   user (formats, class names, config keys). Otherwise bound it (LRU, max size) or move it to a
   real cache (PSR-6/PSR-16, APCu, Redis) and reset it.
5. Statics: replace them with a property of a shared service that is reset, or a real cache.

**False positive**: the value is computed once from configuration (lazy init at boot), the class
is not shared (a DTO or entity scanned without the container map), or the write is cleaned up in
`finally`. Use `#[WorkerSafe(reason: ...)]` on the property.

## DetectSingletonMutation

**Messages**: `Mutation detected on an injected dependency ($this-><dep>)`,
`Mutation detected on a local reference to a shared service ($<var>)`.

**Why**: a call such as `set*`, `add*`, `push*`, `register*`, `enable*`, `disable*`, `clear*`,
`remove*` on a shared dependency changes it for every request. Typical: `$em->getFilters()->disable('softdeleteable')`, `$translator->setLocale()`, `$client->setDefaultHeader()`.

**Fix**: restore the previous state in `finally`
(`$filters->disable('x'); try { ... } finally { $filters->enable('x'); }`), use an API that takes
the value per call (`$translator->trans($id, [], null, $locale)`), or build a dedicated instance
(clone, factory) for this request.

**False positive**: the dependency is reset between requests. Igor already ignores dependencies
it knows are resettable (Symfony `kernel.reset`); without the container map it cannot know.

## DetectClosureStateLeak

**Message**: `Potential Memory Leak: Injection of a closure capturing local state into a shared service.`

**Why**: `$dispatcher->addListener('x', function () use ($data) {...})` registers one more closure
on every request. The list grows forever and each closure keeps its captured data alive.

**Fix**: register the listener once (event subscriber, `boot()`), and pass the data through the
event or a request attribute. If a per-request callback is needed, remove it in `finally`.

**Severity**: igor reads the method the closure is passed to (vendor code included, an interface
resolved to its service through the container map). It reports an ERROR when that method stores
the closure, and nothing when it only calls it (`wrapInTransaction()`, `CacheInterface::get()`).
A WARNING means igor could not read the method (interface without a container map, abstract
class, untyped property, named argument) and its name does not say it registers a callback: read
the method yourself.

**False positive**: a WARNING on a method that calls the closure and returns. Add it to the
baseline, or type the property with the concrete class so igor can read the method.

## StaleDependency (Laravel)

**Messages**: `Property '<prop>' of <Class> keeps the HTTP request (...)`,
`... keeps the application container (...)`, `... keeps the configuration repository (...)`.

**Why**: Octane clones the request, container and config for every request, but a singleton built
at boot keeps the first request's copy for the whole worker life.

**Fix**: `request()`, `app()` or `config()` when the value is needed, pass the request to the
method that uses it, or inject a closure (`fn () => app('request')`). With Octane, making the
service `scoped()` also works, but only if no persistent singleton keeps it: see
`laravel-octane.md`.

## IncompleteReset

**Message**: `Property '<prop>' of <Class> is mutated but not reset in reset().`

**Fix**: clear that property in `reset()` too (to its initial value). If it is a bounded cache that
must survive, mark the property `#[WorkerSafe(reason: ...)]`.

## MissingResetTag (Symfony)

**Message**: `<Class> has reset()|clear() but its service is never reset between requests (missing kernel.reset tag).`

**Fix**: tag the service so `services_resetter` calls the method after each request:
`->tag('kernel.reset', ['method' => 'reset', 'on_invalid' => 'ignore'])` or, with autoconfigure,
implement `Symfony\Contracts\Service\ResetInterface`.

## UnsafeStateRestore

**Messages**: `Property '<prop>' is restored without finally: an exception thrown after line N leaves it set for the next requests.`,
`Property '<prop>' is restored in catch (\Exception): an \Error leaves it set for the next requests.`

**Fix**: move the restoration into `finally` (or catch `\Throwable`):
```php
$previous = $this->locale;
$this->locale = $locale;
try {
    return $this->render();
} finally {
    $this->locale = $previous;
}
```

## DoctrineFlushState (Symfony / Doctrine)

**Message**: `Property '<prop>' collected in onFlush() is only cleared in postFlush(), which Doctrine skips when the commit fails: ...`

**Fix**: clear the property at the start of the collecting event (`onFlush`, `preFlush`), or also
in `onClear`, or make the listener resettable (`ResetInterface` + `kernel.reset`).

## ProcessStateMutation

**Messages**: `Function '<fn>' modifies the global PHP process ...` (`date_default_timezone_set`,
`setlocale`, `ini_set`, `chdir`, `umask`, `mb_internal_encoding`, `gc_disable`, `set_error_handler`...).

**Why**: the PHP process is shared by every request of the worker.

**Fix**: pass the value explicitly (`new \DateTimeZone(...)`, `mb_strlen($s, 'UTF-8')`, absolute
paths, `chmod()`), set it once in configuration (`php.ini`, FrankenPHP `php_ini`), or restore
the previous value in `finally`: `restore_error_handler()` after `set_error_handler()`, and for
`ini_set()` the value it returned (`$previous = ini_set('x', $v); try { ... } finally { if ($previous !== false) { ini_set('x', $previous); } }`).
Not `ini_restore()`: it goes back to the startup value, not to the value set before.

**False positive**: the call runs only at boot (kernel boot, service provider `register()`) with a
constant value.

## LocalStaticVariable

**Message**: `Usage of local static variable is dangerous in Worker mode.`

**Fix**: a property of the (reset) service, or a real cache. Acceptable only for a bounded
memoization independent of the request.

## SuperglobalsUsage

**Messages**: `Usage of PHP Superglobal $_GET|$_POST|$_SERVER|$_COOKIE|$_FILES|$_SESSION|$_REQUEST is discouraged in Worker mode.`

**Fix**: the framework request (`Request $request` argument, `RequestStack::getCurrentRequest()`,
Laravel `request()`). Reading `$_ENV` is allowed; writing it is not.

## ExecutionTerminator

**Message**: `Usage of exit/die is forbidden in Worker mode.`

**Fix**: return a response or throw an exception (an `HttpException` / `abort()` in Laravel).
`exit` stops the worker, which then restarts with the cost of a full boot.

## MagicMethodDestruct

**Message**: `Usage of __destruct() magic method is dangerous in Worker mode.`

**Why**: a shared service is destroyed when the worker stops, not after each request: cleanup in
`__destruct()` (flush, close, unlock) never runs per request.

**Fix**: move the cleanup to a reset (`ResetInterface`, Octane listener), to `kernel.terminate`, or
to a `finally` in the method that opens the resource.
