# Accepting a finding: false positives, vendor code, baseline

Igor is strict on purpose: it reports everything that may survive a request. Accept a finding only
after reading the code, and always leave the reason where the next developer will see it.

## Narrowest first

1. **Property, method or class** that is safe by design:
   ```php
   use IgorPhp\IgorBundle\Attribute\WorkerSafe;

   #[WorkerSafe(reason: 'Filled once from config at boot, never written during a request')]
   private array $formats = [];
   ```
   Also on a method (`warmUp()`), or the class (`scope: 'boot-time'`). Works on Laravel too.
2. **One line**: `$this->x = $y; // @igor-ignore - <reason>`
3. **Baseline**, for legacy code you will not fix now, or a project you take over:
   ```bash
   vendor/bin/igor-php --generate-baseline .   # writes igor-baseline.json
   vendor/bin/igor-php --check-baseline .      # fails if an entry is no longer detected
   vendor/bin/igor-php --prune-baseline .      # removes the stale entries
   ```
   Every generated entry has `"reason": "TODO: ..."`. Replace each one with a real reason, or fix the
   finding. `--include-ignored --output json` shows the baselined findings with their reason.

## Common false positives

- **Not a service**: entities, DTOs, value objects, form requests, jobs, events. With the container
  map igor skips them; without it (`--no-agent`, app not booting) they show up. Fix the map rather
  than ignoring them, or exclude them from autowiring (Symfony).
- **Lazy init from config**: `$this->parser ??= new Parser()` with no request input is already
  ignored; the same with a config value is safe, mark it `#[WorkerSafe]`.
- **Bounded memoization**: a cache keyed by class name, format or config key. Safe if the key space
  is finite and not chosen by the user.
- **Already reset**: the service is reset by a mechanism igor does not see (a custom listener on
  `kernel.request`, a decorator). Prefer the standard mechanism so igor sees it.

## Vendor code

You cannot fix a package. Options: check the package's issue tracker or a newer version, report
it upstream with a reproducer, add it to `octane.flush` (Laravel) if it can be rebuilt, decorate it
with a resettable wrapper, or as a last resort restart workers regularly (FrankenPHP `max_requests`,
Octane `--max-requests`). Packages can ship their own `igor-baseline.json`; igor loads them.
