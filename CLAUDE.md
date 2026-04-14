# Company Town

Multi-agent development orchestration system. Go + Dolt.

## Build & Test

```bash
make build          # Build ct and gt binaries to bin/
make install        # Install ct and gt to $GOPATH/bin
go test ./...       # Run all tests
```

## Key Packages

| Package | Purpose |
|---------|---------|
| `cmd/ct/` | User CLI entry point |
| `cmd/gt/` | Agent CLI entry point |
| `internal/commands/` | ct command implementations + agent CLAUDE.md templates |
| `internal/config/` | Config types and loader |
| `internal/db/` | Dolt server connection, embedded migrations |
| `internal/repo/` | Issue and agent database operations |
| `internal/session/` | Tmux session management |

## Database

Dolt (MySQL-compatible). Server config: `.company_town/dolt-data/config.yaml`.
Tables: `issues`, `agents`, `issue_dependencies`.
Migrations embedded in `internal/db/migrations/`.

## Linting

`golangci-lint` runs in CI with the config in `.golangci.yml`. The enabled linter set is intentionally narrow — see the comments in that file before adding new linters. To run locally:

```bash
golangci-lint run ./...
```

## Quality Targets

Quality checks run automatically via the daemon and can be triggered manually with `gt check run`. The `threshold` and `warn_threshold` fields in `config.json` are **targets**, not hard CI gates — they define the range where the project is healthy.

For `Direction: "lower"` checks (fewer = better): `threshold` is the ideal upper bound (pass), `warn_threshold` is the acceptable upper bound (warn). Exceeding `warn_threshold` is a fail.

Default targets for this project:

| Check | Target (pass) | Warn | Direction | Notes |
|-------|--------------|------|-----------|-------|
| `go_test_coverage` | ≥ 60 % | ≥ 50 % | higher | Brackets today's ~53% reality |
| `lint_warning_count` | ≤ 0 | ≤ 10 | lower | Baseline established via nc-122 |
| `loc_total` | ≥ 1 000 | — | higher | Informational trend metric |
| `todo_count` | ≤ 0 | ≤ 5 | lower | TODOs/FIXMEs/XXXs in Go files |
| `test_count` | ≥ 50 | ≥ 20 | higher | Total `func Test*` functions |
| `dependency_count` | ≤ 50 | ≤ 75 | lower | Third-party modules |
| `open_ticket_count` | ≤ 10 | ≤ 20 | lower | Non-closed, non-cancelled tickets |

Adjust thresholds in `.company_town/config.json` as the project grows; commit the change so history reflects deliberate target shifts, not drift.

## Worktrees

Company Town uses a bare-clone + per-agent-worktree model introduced in nc-128.

**Layout:**

```
.company_town/
├── config.json          — project config (dolt connection, models, prole cap)
├── db/                  — Dolt database directory (server reads/writes here)
├── repo.git/            — bare clone; all worktrees share this object store
├── agents/
│   ├── architect/
│   │   ├── CLAUDE.md    — architect instructions (redeployed on every gt start)
│   │   ├── memory/      — handoff.md and other persistent state
│   │   └── worktree/    — isolated git worktree for architect
│   ├── mayor/worktree/
│   └── reviewer/worktree/
└── proles/
    ├── copper/          — prole worktree (on its own feature branch)
    └── tin/             — prole worktree
```

**How agents find `.company_town/`:** `gt`/`ct` call `db.FindProjectRoot()` which walks up from `os.Getwd()` looking for a `.company_town/` directory. This works correctly from any worktree depth — agents never need to `cd` to the project root.

**Dolt safety:** Never use `dolt sql -q` or `dolt sql --query` shellouts from a worktree. Those commands look for `.dolt/` relative to CWD, which does not exist in worktrees. All application SQL goes through the `database/sql` TCP connection to the running Dolt server. A direct shellout silently reads stale or empty data.

**Pre-commit hooks:** `agentworktree.Ensure` and `prole.Create` copy `scripts/pre-commit` into each worktree's gitdir hooks directory on creation. Worktree `.git` entries are files (not directories), so the hook lands in `<bare>/worktrees/<name>/hooks/pre-commit`.

**Teardown:** `ct stop --clean` removes prole worktrees and prunes the bare repo, but leaves agent worktrees intact. `ct nuke` removes everything: prole worktrees, agent worktrees, and the bare clone (`repo.git/`).

**Recovery:** If a prole worktree is corrupted or a branch is lost, use `gt prole reset <name>` to recreate it from `origin/main`. If the bare clone is missing, `agentworktree.Ensure` (called by `gt start`) recreates it automatically.

**Migration note:** Projects created before nc-128 have no bare clone or agent worktrees. Running `ct start` or `gt start architect` creates them on demand.

## Rules

- Never push to main — feature branches + PRs
- Commit and push frequently
- No stubs, no hacks, complete implementations only
