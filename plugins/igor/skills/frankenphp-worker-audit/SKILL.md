---
name: frankenphp-worker-audit
description: Audit a Symfony or Laravel (Octane) project for FrankenPHP worker mode with igor-php, then fix the state leaks it reports. Use when the user wants to run their PHP app in worker mode (FrankenPHP, Octane, RoadRunner, Swoole), asks to audit or fix memory leaks or state pollution between requests, or mentions igor / igor-php.
---

# FrankenPHP worker audit with igor-php

In worker mode, one PHP process serves many requests and keeps every shared service in memory.
Whatever a service writes in a property, a static, or the PHP process (timezone, locale, cwd...)
is still there for the next request, often another user's. igor-php finds these places
statically. Your job: install it if needed, run it, sort what it reports, fix the real leaks,
and justify the rest, without making the user learn the tool.

Work in this order and keep the user informed in plain words at each step.

## 1. Set up (only what is missing)

1. **Framework.** `bin/console` → Symfony. `artisan` → Laravel (check `laravel/octane` in
   `composer.json`: without it, igor uses the plain worker model where every `singleton()` and
   `scoped()` persists). Neither → plain PHP: igor scans the directories of `igor.json`.
2. **Binary.** Use `vendor/bin/igor-php` if it exists, else `igor-php` or `igor` on the PATH.
   Otherwise install it: `composer require --dev igor-php/igor-php` (the Composer package
   downloads the matching binary on first run). Ask before installing if the user did not ask
   for an audit outright.
3. **Container map.** It removes most false positives, so set it up:
   - Symfony: register `IgorPhp\IgorBundle\IgorPhpBundle::class => ['dev' => true, 'test' => true]`
     in `config/bundles.php`, then `php bin/console cache:clear`.
   - Laravel: nothing to do, package discovery registers `IgorServiceProvider` and igor runs
     `php artisan igor:dump` itself.
4. **Config.** No `igor.json`? Run `vendor/bin/igor-php init`.

## 2. Run the audit

```bash
vendor/bin/igor-php --output json .        # whole project
vendor/bin/igor-php --output json src/Service/Foo.php   # one file
```

Flags always go **before** the target: igor stops reading flags at the first path.
The JSON is a list of files (`file_path`, `service_id`, `is_shared`) with their `findings`
(`line`, `severity`, `message`, `remediation`, `context_class`, `context_method`,
`reachability`, `category`, `property`, `cache_key`). Banners and errors go to stderr.

If the deep audit cannot start (Symfony cache not warmed up, `igor:dump` failing because the
app does not boot), say so, fix it if it is simple, otherwise rerun with `--no-agent` and warn
that this scan is less precise (it does not know which classes are shared).

On a big project, delegate the sorting to the `igor-triage` agent to keep your context small.

## 3. Sort before fixing

Present a short summary to the user, grouped by service, in this order:

1. `ERROR` findings with `reachability` other than `INFO`: reachable from a request, fix first.
2. Remaining `ERROR` findings.
3. `WARNING` findings (process state, superglobals, incomplete resets).
4. Set aside: findings with `out_of_request` (console, profiler, kernel boot), shown only with
   `--show-all`, and vendor code you cannot change (see `references/false-positives.md`).

For each finding, decide with the code in front of you:
- **Real leak**: request or user data, or a value derived from it, survives the request.
- **Bounded cache**: memoization keyed by a finite set (formats, class names, config keys).
  Still state, but harmless if the key space is bounded and does not depend on the user.
- **False positive**: the property is written once at boot, or the class is not shared.

Ask the user before large refactorings; small local fixes can go ahead.

## 4. Fix, one service at a time

Find the fix for each finding in `references/rules.md` (matched on the message), with the
framework details in `references/symfony.md` or `references/laravel-octane.md`.

General principles:
- Prefer making the service stateless: pass the value as a method argument, or read it from the
  current request when needed.
- If the state is wanted per request, reset it between requests (Symfony: `ResetInterface` +
  `kernel.reset` tag; Laravel: `scoped()` binding or a `reset()` called from an Octane listener).
- A temporary value must be restored in `finally`, never after code that can throw.
- Never fix a finding by hiding it (`@igor-ignore`, `#[WorkerSafe]`, baseline) when the leak is
  real.

After each fix, re-audit the file (`vendor/bin/igor-php --output json <file>`) and check that
the finding is gone and no new one appeared. Run the project tests if there are any.

## 5. Accept what is safe, with a reason

For a finding that is safe on purpose, choose the narrowest tool and always write why:
- `#[WorkerSafe(reason: '...')]` on the property, method or class
  (`use IgorPhp\IgorBundle\Attribute\WorkerSafe;`, works for Laravel too).
- `// @igor-ignore - <reason>` on one line.
- A baseline entry for legacy code you will not touch now: `vendor/bin/igor-php --generate-baseline .`
  then replace every `TODO` reason in `igor-baseline.json` with a real one. Never leave a
  generated baseline unreviewed: it would hide real leaks.

## 6. Wrap up

- Rerun the full audit and report: fixed, accepted (with reasons), left for later.
- If there is a baseline, `vendor/bin/igor-php --check-baseline .` must pass.
- Suggest adding `vendor/bin/igor-php .` to CI, and `/igor:watch on` so each PHP file you edit
  later is audited automatically.
- For a leak worth proving, a test that sends two requests to the same kernel or application
  instance and checks the second one does not see the first one's data is the best guard.
