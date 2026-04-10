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
- **🔍 Deep Audit**: Automatically detects Symfony projects and audits **every shared service** defined in the container, including those in `vendor/` and external bundles.
- **🎯 Surgical Precision**: Detects complex state mutations (`$this->prop[]`, `static::$prop`, increments) without false positives.
- **🧠 Intelligent**: Verifies not just the presence of `ResetInterface`, but ensures all mutated properties are correctly reset.
- **🛡️ Safety First**: Catches dangerous `exit()` or `die()` calls that would kill your workers.
- **🔇 Zero Noise**: Automatically ignores `Symfony\` and `Doctrine\` namespaces, and common data folders (`Entity`, `Dto`, `ApiResource`).

---

## 🛠️ Usage

Install the binary and let Igor audit your Symfony project (ensure `bin/console` is present for Deep Audit):

```bash
igor-php ./my-symfony-project
```
### Deep Audit Mode (Symfony)
When a Symfony project is detected, Igor will:
1. Query the container in **PROD mode** (`--env=prod --no-debug`).
2. Map every **shared service** to its actual source file via PHP Reflection.
3. Perform an exhaustive audit of your code and all its dependencies.

---

## ⚙️ Configuration

You can customize Igor's behavior by creating an `igor.json` file at the root of your project:

```json
{
  "exclude": ["vendor", "tests", "Entity"],
  "safe_namespaces": ["Symfony\\", "Doctrine\\", "My\\Safe\\Namespace\\"]
}
```

- **exclude**: List of directories to skip during indexing.
- **safe_namespaces**: Igor will ignore state mutations in classes starting with these prefixes.

---

## 🙏 Credits & Inspirations

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
