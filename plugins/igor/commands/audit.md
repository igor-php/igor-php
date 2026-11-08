---
description: Audit the project (or a path) for FrankenPHP worker mode with igor-php and fix the leaks
argument-hint: "[path]"
---

Audit this project for FrankenPHP worker mode with igor-php, following the
`frankenphp-worker-audit` skill from start to finish: set up igor-php if it is missing, run the
audit, sort the findings, then propose and apply the fixes one service at a time.

Target: $ARGUMENTS (if empty, the whole project).

Start with a short summary for the user: framework detected, number of findings by severity, the
services to fix first. Then ask whether to fix them all, or only the ones they choose.
