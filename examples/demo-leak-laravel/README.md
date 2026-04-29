# 🧟‍♂️ Igor-Php: Laravel Octane Leak Lab

This project demonstrates how PHP state can "leak" between requests when running **Laravel Octane** with **FrankenPHP** (Worker Mode).

## 🧪 The Experiments

Pick an experiment from the dashboard to see state persistence in action:

1.  **Stateful Service Leak**: A singleton service storing data in an array without being flushed.
2.  **Octane Resettable Service**: Proof that the `flush` mechanism in `config/octane.php` works!
3.  **Static Property Leak**: Static data survives even if the service instance is destroyed.
4.  **Stale Request Piège**: Demonstrates the danger of injecting the `Request` object into a singleton constructor.
5.  **Stale Config Piège**: Demonstrates why you shouldn't capture config values in a singleton constructor.
6.  **Global State Poisoning**: Changing the process timezone affects all subsequent requests.
7.  **The Danger of Exit/Die**: How a simple `exit()` kills the entire worker process.

## 🚀 Quick Start

Start the laboratory using Docker:

```bash
cd examples/demo-leak-laravel
docker compose up -d
```

Then visit [http://localhost:8081](http://localhost:8081) to access the dashboard.

## 🛡️ How Igor-Php helps

Igor-PHP is designed to catch these issues automatically before they hit production. Run the audit on this project:

```bash
# From the project root
igor-php examples/demo-leak-laravel
```

### What will Igor detect?
*   **KO**: The `StatefulService` and `StaticLeakService` (Dangerous state).
*   **KO**: The `StaleRequestService` (Forbidden injection).
*   **OK**: The `ResettableService` (Igor knows it's in the `flush` list!).
*   **WARNING**: The global state modifications and `exit()` calls.

## 🧹 Cleanup
```bash
docker compose down
```
