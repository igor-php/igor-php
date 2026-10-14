@AGENTS.md

## Claude Code

### Parallel work: one git worktree per task

Several features are often in progress at once. Never switch branches or stash in a checkout
that has uncommitted work: start every task in its own worktree, branched from `origin/main`.

```bash
git fetch origin
git worktree add -b feat/my-task ../igor-php-my-task origin/main
# ... work, commit, push, open the PR ...
git worktree remove ../igor-php-my-task
```

Worktrees share the Docker caches (`make docker-*` mounts the parent directory), so
`make docker-ci` works from any of them.

### Subagents (`.claude/agents/`)

| Agent | Role | Writes code |
|---|---|---|
| `igor-rule-dev` | Detection rules in the Go analyzer (`internal/analyzer/`), fixtures, tests | yes |
| `igor-bridge-dev` | Symfony / Laravel bridges (`internal/auditor/`, `src/php/`, `src/laravel/`) | yes |
| `igor-tester` | Writes the missing tests and runs `make docker-ci`, reports failures | tests only |
| `igor-reviewer` | Reviews the diff against `main`: false positives/negatives, conventions | no |
| `igor-docs` | Keeps README, CONTRIBUTING, AGENTS.md and the labs READMEs in sync with the code | Markdown only |
| `igor-symfony-hunter` | Audits the Symfony ecosystem itself with igor, reproduces the leaks, rates the risk, checks upstream | outside the repo (`~/Documents/igor-php/hunts/`) |
| `igor-laravel-hunter` | Same for Laravel, Octane and first-party packages | outside the repo (`~/Documents/igor-php/hunts/`) |

Typical flow: a dev agent implements → `igor-tester` covers and validates → `igor-docs`
updates the documentation → `igor-reviewer` reviews before the PR is opened.

The hunters feed this loop: the false positives and missed leaks they report become tasks for
`igor-rule-dev` / `igor-bridge-dev`. They never open upstream issues or PRs, nor publish a
reproducer, without the user's approval.

### Conventions to keep in mind

- Commits: Conventional Commits, `<type>(<scope>): <description>` (scopes: `analyzer`,
  `auditor`, `laravel`, `symfony`, `bundle`, `reporter`, `config`, `demo-leak`, ...).
- PR descriptions follow `.github/pull_request_template.md`.
- A change in the findings on the labs must update the snapshots (`make docker-update-labs`)
  and their diff must be explained in the PR.
