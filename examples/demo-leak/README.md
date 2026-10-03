# 🧟‍♂️ Igor-Php: Global State Leak Demo

This project demonstrates several ways PHP state can "leak" between requests when running in a persistent environment like FrankenPHP Worker mode.

## 🧪 The Experiments

Start the project using Docker:
```bash
docker compose up -d
```
Then visit [http://localhost:8080](http://localhost:8080) to access the **Igor Leak Lab**.

### 🔍 What to look for?
In this laboratory, **NOTHING** is stored in a database, session, cookie, or cache file. 
Everything you see is stored **exclusively in PHP's RAM**.

1.  **Stateful Service Leak**: Property mutation without ResetInterface.
2.  **Incomplete Reset Leak**: Implementing ResetInterface but forgetting a property.
3.  **Static Property Leak**: How static data survives everything (Class level).
4.  **Global State Poisoning**: Changing process settings (Timezone).
5.  **Memory Pressure**: See how RAM usage grows and STAYS high.
6.  **The Danger of Exit/Die**: Killing the worker thread.
7.  **Shared Service Indirect Mutation**: Disabling filters on an EntityManager instance fetched into a local variable, showing how its state is modified permanently for all future requests.
8.  **Closure State Leak**: Leaking state or request data via unmanaged closures.
9.  **Local Static Variable**: Retaining state inside method execution using static variables.
10. **PHP Superglobals**: Direct pollution of `$_SERVER`, `$_GET`, `$_POST`, etc.
11. **Magic Method __destruct() Bypass**: `__destruct()` never runs in persistent workers until worker exit.
12. **Dangerous Process State Mutations**: Functions altering process-wide C/PHP state (`chdir`, `umask`, `mb_internal_encoding`, `gc_disable`).

## 🛡️ How Igor-Php helps

Igor is designed to catch all these issues automatically. Run it on this project:

```bash
igor-php examples/demo-leak
```

### 🪦 Dead & inlined services

Most lab services are public so they are easy to explore, but `src/Model/` and `src/Internal/` are registered as private services, like in a standard Symfony application. They show how the `IgorPhpBundle` service map follows what the compiled container actually keeps:

| Class | What Symfony does | What Igor does |
|---|---|---|
| `App\Model\Order` | Never injected: removed by `RemoveUnusedDefinitionsPass` | Not audited (it never exists at runtime) |
| `App\Internal\OrphanStatefulHelper` | Never injected: removed by `RemoveUnusedDefinitionsPass` | Not audited (it never exists at runtime) |
| `App\Internal\InlinedRequestCounter` | Injected only into `InlinedConsumerService`: inlined into that singleton | Audited as a shared service (`inlined.*` entry), its `$count` mutation is reported |

## 🩺 Runtime Leak Watch

The lab also demonstrates Igor's experimental runtime watcher: instead of reading the code, it snapshots the live services between requests and reports the state that survived. Its test suite replays the experiments above against a kernel that stays alive between requests, like a worker:

```bash
make test
```

- `tests/Functional/LeakLabRuntimeTest.php` requests each experiment and asserts the leak it leaves behind (growing cache, incomplete reset, static property, local static, captured closure, nested object mutation, timezone and process state).
- `tests/Runtime/ServiceSnapshotterTest.php` shows what the snapshot walker sees and what it refuses to touch (lazy objects, other services, `#[WorkerSafe]` properties).

To see what a report looks like, run a test that fails on purpose after browsing several experiments:

```bash
make leak-demo
```

In worker mode (`make start-worker`), the watcher is active too: browse a few experiments, then read `var/log/igor-leaks.jsonl`. Each line is the report of the previous request, written when the next one starts.

## 🧹 Cleanup
```bash
docker compose down
```
