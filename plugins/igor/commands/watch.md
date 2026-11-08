---
description: Turn on or off the automatic igor-php audit of each PHP file Claude edits
argument-hint: "[on|off]"
allowed-tools: Bash(mkdir -p .claude), Bash(touch .claude/igor-watch), Bash(rm -f .claude/igor-watch), Bash(test -f .claude/igor-watch)
---

The igor plugin has a hook that audits, with igor-php, every PHP file you edit and reports the
findings on the lines you changed. It runs only when `.claude/igor-watch` exists in the project
(or when the `IGOR_WATCH` environment variable is `1`).

Requested state: $ARGUMENTS

- `on`: run `mkdir -p .claude && touch .claude/igor-watch`.
- `off`: run `rm -f .claude/igor-watch`.
- empty: report the current state (`test -f .claude/igor-watch`) and how to change it.

Then tell the user in one or two sentences:
- the new state;
- that the file can be committed to turn the watch on for the whole team, or added to
  `.gitignore` to keep it personal;
- if `vendor/bin/igor-php` is missing and neither `igor-php` nor `igor` is on the PATH, that the
  hook stays silent until igor-php is installed (`composer require --dev igor-php/igor-php`).
