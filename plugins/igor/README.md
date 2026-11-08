# igor: Claude Code plugin for igor-php

Makes Claude Code audit a Symfony or Laravel (Octane) project for FrankenPHP worker mode with
[igor-php](https://github.com/igor-php/igor-php), and fix the state leaks it finds.

## Install

```text
/plugin marketplace add igor-php/igor-php
/plugin install igor@igor-php
```

Then ask Claude to "audit my project for FrankenPHP worker mode", or run `/igor:audit`. Claude
installs igor-php (`composer require --dev igor-php/igor-php`) and the Symfony bundle if they are
missing.

## Content

| Part | What it does |
|---|---|
| Skill `frankenphp-worker-audit` | The whole workflow: set up, audit, sort, fix one service at a time, re-audit, accept the rest with a written reason. Loaded automatically when you talk about worker mode, Octane or memory leaks. |
| `references/` | One section per igor rule with the Symfony and Laravel fix, the framework specifics, and how to accept a false positive. |
| `/igor:audit [path]` | Runs the skill on the project or a path. |
| `/igor:watch [on\|off]` | Turns on the hook below for this project (creates `.claude/igor-watch`). |
| Hook `PostToolUse` | Once the watch is on: after each edit of a PHP file, audits it and tells Claude about the findings on the lines it just wrote. It never blocks, and stays silent if igor-php is not installed. |
| Agent `igor-triage` | Sorts a large audit (real leak, cache, false positive...) without filling the main context. |

The hook needs `php` on the PATH. It uses `vendor/bin/igor-php`, then `igor-php` or `igor` on the
PATH; set `IGOR_BIN` to use another binary, and `IGOR_WATCH=1` (or `0`) to override
`.claude/igor-watch`.
