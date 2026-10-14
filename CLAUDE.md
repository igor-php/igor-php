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

Typical flow: a dev agent implements → `igor-tester` covers and validates → `igor-reviewer`
reviews before the PR is opened.

### Conventions to keep in mind

- Commits: Conventional Commits, `<type>(<scope>): <description>` (scopes: `analyzer`,
  `auditor`, `laravel`, `symfony`, `bundle`, `reporter`, `config`, `demo-leak`, ...).
- PR descriptions follow `.github/pull_request_template.md`.
- A change in the findings on the labs must update the snapshots (`make docker-update-labs`)
  and their diff must be explained in the PR.
