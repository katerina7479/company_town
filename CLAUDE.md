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

## Rules

- Never push to main — feature branches + PRs
- Commit and push frequently
- No stubs, no hacks, complete implementations only
