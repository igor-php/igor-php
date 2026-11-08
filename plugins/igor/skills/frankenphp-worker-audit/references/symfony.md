# Symfony in FrankenPHP worker mode

## What persists

In worker mode the kernel boots once. Every **shared** service (the default) lives for the whole
worker. Between two requests, Symfony calls `services_resetter`, which runs the reset method of the
services tagged `kernel.reset`. Nothing else is cleared.

## The igor bundle

`IgorPhp\IgorBundle\IgorPhpBundle` writes `var/cache/<env>/igor_service_map.json` when the
container compiles: for each service its class, `shared`, `resettable` and its injected services.
Igor then audits only shared services and their parents and traits, ignores mutations on resettable
dependencies, and follows interface aliases. Register it for `dev` and `test` in
`config/bundles.php`, then `php bin/console cache:clear`. Without the map, igor stops and asks
for `cache:clear`, or scans everything with `--no-agent`.

## Fix toolbox

**Reset a service between requests**
```php
use Symfony\Contracts\Service\ResetInterface;

final class CartCalculator implements ResetInterface
{
    private array $lines = [];

    public function reset(): void
    {
        $this->lines = [];
    }
}
```
With autoconfigure (the default), implementing `ResetInterface` adds the `kernel.reset` tag. A
service declared by hand, or a reset method with another name, needs the tag:
```yaml
App\Service\Buffer:
    tags: [{ name: kernel.reset, method: clear, on_invalid: ignore }]
```

**Non-shared service**: `shared: false` builds a new instance at each injection. Only for cheap
services injected where they are used; it does not help a service injected once in a shared one.

**Current request**: inject `RequestStack` and call `getCurrentRequest()` when needed, never store
the request or a value derived from it (locale, user, host) in a property.

**Current user**: `Security::getUser()` / `TokenStorageInterface::getToken()` at call time.

**Doctrine**: the `EntityManager` is reset by `doctrine` (resettable), but filters you disable,
listeners you add and entities you keep in properties are not. Re-enable filters in `finally`.

**Excluding non-services**: if entities, DTOs or models show up in the audit, they are registered
as services. Exclude them from `App\` autowiring in `config/services.yaml`
(`exclude: '../src/{Entity,Dto,Model}'`).

## Boot-time code

Findings in console commands, cache warmers, compiler passes, the profiler and kernel boot are
flagged `out_of_request` and folded by default: they do not run while a request is served.
