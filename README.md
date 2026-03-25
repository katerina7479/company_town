# Company Town

Multi-agent development orchestration system. One instance per project, one database per project. Manages a team of AI agents with defined roles, a ticket lifecycle, git worktrees per prole, and a Dolt database for state.

## Requirements

- Go 1.22+
- [Dolt](https://docs.dolthub.com/introduction/installation)
- tmux

## Quick Start

```bash
make build
./bin/ct init        # Set up .company_town/ in your project root
# Edit .company_town/config.json (set github_repo, ticket_prefix)
./bin/ct start       # Start the Mayor
```

## CLI

### User CLI (`ct`)

| Command | Description |
|---|---|
| `ct init [--force]` | Set up `.company_town/` in project root |
| `ct start` | Start the Mayor and attach to tmux session |
| `ct stop` | Graceful shutdown with handoffs |
| `ct nuke` | Immediate shutdown, no handoffs |
| `ct architect` | Start the Architect |
| `ct architect stop` | Stop the Architect gracefully |

### Agent CLI (`gt`) — internal use by agents

| Command | Description |
|---|---|
| `gt ticket <create\|assign\|status\|close>` | Manage tickets |
| `gt prole <create\|reset>` | Manage proles |
| `gt agent <register\|status>` | Manage agents |
| `gt pr <create>` | File PRs |
| `gt status` | Print system status |

## Architecture

See the system specification for full details. Roles:

- **Mayor** (Opus) — human-facing, manages system
- **Architect** (Opus) — designs and specs tickets
- **Artisan** (Opus) — specialty coders (frontend, backend, qa)
- **Conductor** (Sonnet) — assigns tickets to workers
- **Prole** (Sonnet) — ephemeral implementation agents
- **QA** (Sonnet) — reviews PRs
- **Janitor** (Sonnet) — cleanup and maintenance patrols
