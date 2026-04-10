# 🧟‍♂️⚡ Igor-Php 
<p align="center">
  <img src="igor-php.png" alt="Igor PHP Logo" width="600">
</p>

**The faithful assistant for your FrankenPHP Workers.**

`igor-php` is an ultra-fast static linter written in **Go** that prepares your **Symfony** application for the persistent memory model of **FrankenPHP**.

Like the legendary assistant, `igor` checks every connection and part of your application to ensure it won't "blow up" when the lightning strikes (Worker Mode).

---

## ✨ Highlights

- **⚡ Lightning Fast**: Scans hundreds of files in milliseconds using Go's native multi-threading.
- **🎯 Surgical Precision**: Detects complex state mutations (`??=`, `list()`, `[]`, increments) without false positives.
- **🧠 Intelligent**: Verifies not just the presence of `ResetInterface`, but ensures all mutated properties are correctly reset.
- **🛡️ Safety First**: Catches dangerous `exit()` or `die()` calls that would kill your workers.

---

## 🛠️ Usage

Install the binary and let Igor audit your source code (usually `src`):

```bash
igor ./src
```

### Example Output
```text
📂 src/Service/MyService.php
  ❌ Mutation of state 'cache' in MyService::getData().
  42 | $this->cache = $result;

⚠️  Property 'tempData' of MyService is mutated but not reset in reset().
  💡 Hint: Add '$this->tempData = null;' in the reset() method.
```

---

## 🚀 Installation

### Via Go
```bash
go install github.com/KevinMartinsDev/igor-php@latest
```

---

## 🙏 Credits & Inspirations

- **[Phanalist](https://github.com/denzyldick/phanalist)**: Special thanks to `phanalist` and its rule `E0012` (Stateful Service) which inspired Igor's core mutation detection logic.
- **[Gemini CLI](https://github.com/google/gemini-cli)**: This project was built with the help of Gemini CLI.
- **[FrankenPHP](https://frankenphp.dev/)**: For the amazing server that makes these checks necessary!

---

## 📄 License
MIT
