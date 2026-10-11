# 🧟‍♂️ Igor-Php: Global State Leak Demo

This project demonstrates several ways PHP state can "leak" between requests when running in a persistent environment like FrankenPHP Worker mode.

## 🧪 The Experiments

Start the project using Docker (only Docker is required: Composer runs inside the FrankenPHP image):
```bash
make start
```
This builds the image, runs `composer install` in a container and starts both services.
Then visit [http://localhost:8080](http://localhost:8080) to access the **Igor Leak Lab**.

### 🔍 What to look for?
In this laboratory, no experiment state is stored in a database, session, cookie, or cache file.
The leak demonstrations use **PHP's RAM only**, except experiment 11, which writes a disk-backed log (`var/destructor_demo.log`) to show when constructors and destructors run.

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

## 🧹 Cleanup
```bash
docker compose down
```
