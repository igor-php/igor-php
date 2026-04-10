# 🚀 Project: `franken-check` (Go)

Static linter for FrankenPHP **Worker Mode**.

## 🎯 Goal & Vision
Provide an ultra-fast CLI tool, written in Go, capable of certifying that a PHP application is compatible with the persistent memory model of **FrankenPHP**, **Swoole**, or **RoadRunner**.

**Long-term Vision:** Submit `franken-check` as an official tool for the FrankenPHP ecosystem (under `php/frankenphp` or `dunglas` organizations), potentially integrated as a sub-command of the main binary (`frankenphp check`).

---

## 🏛️ "Official-Ready" Acceptance Criteria
To be adopted by the official community, the tool must follow:
1. **Extreme Performance**: Near-instant scan via Go's native multi-threading.
2. **Zero False Positives**: Fine-grained AST analysis to avoid unnecessary alerts (e.g., `readonly` properties or `ResetInterface` usage).
3. **Actionable Insights**: Every error must point to remediation documentation.
4. **Zero Dependencies**: A single static binary, easy to integrate into any CI.

---

## 🛠️ Technical Specifications

- **Language**: Go (Golang) 1.21+
- **Parser**: Tree-sitter (official PHP grammar)
- **Concurrency**: Goroutines + Worker Pools for multi-threaded scanning.
- **Output Formats**: Console (Colored), JSON (WIP), SARIF (GitHub Actions).

---

## 🔍 Analysis Rules

### 1. Stateful Service Check
*   **Description**: Detects object property mutations (`$this->prop = $val`) in services/controllers.
*   **Exception**: 
    - `__construct` method.
    - Classes implementing `ResetInterface` (requires checking consistency).
*   **Remediation**: Make properties `readonly` or implement state reset.

### 2. Static Mutation Check
*   **Description**: Forbids assigning values to static properties (`self::$cache = $val`).
*   **Why**: Static variables persist for the entire lifetime of the Go/PHP worker.
*   **Remediation**: Use a Cache service (Redis/APC) or configuration parameters.

### 3. Execution Terminator Check
*   **Description**: Detects usage of `die()` and `exit()`.
*   **Why**: Kills the worker prematurely, causing expensive restarts and potential server crashes via FrankenPHP's exponential backoff.
*   **Remediation**: Throw an exception or return a HTTP `Response`.

### 4. Global State Access
*   **Description**: Flags usage of `global` keyword or direct access to superglobals.
*   **Remediation**: Use the Request object provided by the framework (Symfony/Laravel).

---

## 📚 References
- [FrankenPHP Worker Documentation](https://frankenphp.dev/docs/worker/)
- [PR #1951 - State Warning Documentation](https://github.com/php/frankenphp/pull/1951)
