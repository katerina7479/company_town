# Company Town

A local, self-hosted multi-agent development system. One instance per project, one Dolt database per project. Company Town manages a small team of AI agents with defined roles, a ticket lifecycle, git worktrees per worker, and a Dolt database for state. All runtime state lives under `.company_town/` in your project — nothing leaves the machine except the PRs your agents file on GitHub.

You talk to the **Mayor** in a tmux pane. The Mayor and its daemon run the rest: the **Architect** specs tickets, the **daemon** assigns them directly, **Proles** implement them in isolated worktrees, and the **Reviewer** AI-reviews PRs before a human merges.

## Requirements

- [Dolt](https://docs.dolthub.com/introduction/installation)
- tmux
- `gh` (GitHub CLI), authenticated against the repo you want agents to push to
- `claude` CLI (Claude Code), authenticated

## Getting Started

### 1. Download and install

Go to the [latest release](https://github.com/katerina7479/company_town/releases/latest) and grab the archive for your platform:

| Platform | Archive |
|---|---|
| macOS Apple Silicon | `company_town_X.Y.Z_darwin_arm64.tar.gz` |
| macOS Intel | `company_town_X.Y.Z_darwin_amd64.tar.gz` |
| Linux amd64 | `company_town_X.Y.Z_linux_amd64.tar.gz` |

Extract and put the binaries on your PATH:

```bash
# example for macOS arm64 — adjust filename for your platform and version
tar xz -f company_town_X.Y.Z_darwin_arm64.tar.gz
sudo mv ct gt /usr/local/bin/
```

Each release includes a `checksums.txt` you can use to verify the download.

### 2. Verify the install

```bash
ct --version
gt --version
```

Both should print the version string (e.g. `ct version 0.1.0`).

### 3. Initialize a project

From the root of the project you want agents to work on:

```bash
cd ~/my-project
ct init             # creates .company_town/, starts Dolt, runs migrations
```

### 4. Configure

Open the generated config file and fill in the required fields:

```bash
$EDITOR .company_town/config.json
```

Key fields to set:

| Field | Example | Notes |
|---|---|---|
| `ticket_prefix` | `"nc"` | Short prefix used in branch names and PR titles. |
| `github_repo` | `"git@github.com:you/my-project.git"` | Where proles push branches. |
| `agents.mayor.model` | `"claude-opus-4-5"` | Model for the Mayor and Architect. |
| `agents.prole.model` | `"claude-sonnet-4-5"` | Model for Proles and the Reviewer. |

### 5. Start

```bash
ct start            # starts the daemon and attaches you to the Mayor's tmux pane
```

From inside the Mayor pane, describe what you want built. The Mayor files a draft ticket; the Architect specs it; the daemon assigns it to a prole; the prole implements and files a PR; the Reviewer checks it; you merge on GitHub.

To tear down:

```bash
ct stop             # graceful: agents write handoffs, exit cleanly
ct nuke             # immediate: kills every tmux session, no handoff
```

Neither command drops the database. Re-running `ct start` picks up where you left off.

## Install from source

For contributors building from the repository:

```bash
git clone https://github.com/katerina7479/company_town.git
cd company_town
make install        # builds and installs ct + gt to $GOPATH/bin
```

Requires Go 1.22+. `make build` alone drops the binaries at `./bin/ct` and `./bin/gt`.

## How it works

### Agents

| Agent | Model | Lifetime | Responsibility |
|---|---|---|---|
| **Mayor** | opus | long-lived | Human interface. Starts/stops other agents, handles escalations, receives merge notifications. |
| **Architect** | opus | long-lived | Picks up `draft` tickets, investigates the codebase, writes specs under `.company_town/ticket_specs/`, moves tickets to `open`. |
| **Daemon** | — | background | Watches `open` + `repairing` tickets and assigns them to proles. Runs automatically with `ct start`. |
| **Reviewer** | sonnet | long-lived | AI-reviews PRs when tickets hit `in_review`, posts GitHub review, moves ticket to `pr_open` or `repairing`. |
| **Proles** | sonnet | ephemeral | Implementation workers. One ticket at a time, one git worktree each. Named after metals (`copper`, `iron`, `tin`…). |
| **Artisans** | opus | long-lived | Long-running specialists (`backend`, `frontend`, `qa`). Pick up work their specialty matches. |

Agents talk to the system through the internal `gt` CLI. Users talk to the Mayor, not to `gt`.

### Ticket lifecycle

```
draft ─► open ─► in_progress ─► in_review ─► under_review ─► pr_open ─► closed
  │       │                                      │             │
  │       └──────── repairing ◄──────────────────┴─────────────┘
  │                    │
  │                    └──► in_review
  └──► on_hold
```

| Status | Owner | Meaning |
|---|---|---|
| `draft` | Architect | Created but not specced. |
| `open` | Daemon | Specced, unblocked, ready for a prole. |
| `in_progress` | Prole | Prole is implementing. Branch exists. |
| `in_review` | Reviewer | PR filed, waiting for the Reviewer agent to pick it up. |
| `under_review` | Reviewer | Actively being reviewed. |
| `pr_open` | Human | Reviewer approved. Human reviews and merges on GitHub. |
| `repairing` | Prole | Reviewer or human requested changes. Prole fixes and re-pushes. |
| `closed` | Daemon | PR merged (auto-detected) or manually closed. |
| `on_hold` | Any | Blocked by an external input. |

Epics are containers, never workable. Proles don't touch them.

### Selection order

When the daemon has idle prole slots and selectable work, it picks tickets in this order (strict lexicographic):

1. `repairing` before `open`
2. bugs before tasks before anything else
3. P0 → P1 → P2 → P3 → null
4. lower id first

This is deterministic — no LLM is involved in the pick.

### Daemon

A background goroutine inside `ct start` that polls every 30 seconds (configurable via `polling_interval_seconds`). Each tick it:

- Restarts dead architect/reviewer sessions when they have work to do
- Nudges architect about `draft` tickets
- Assigns idle proles to the top selectable tickets
- Detects PR merges → closes tickets
- Detects human PR review comments → moves tickets to `repairing`
- Detects dead tmux sessions → marks agents `dead`
- Runs quality baseline checks (build / test / vet) on the configured cadence

Daemon output lives at `.company_town/logs/daemon.log`.

## Daily loop

**As the user** — you never type `gt` commands directly. You run `ct start` once a day (or once a week, or whenever), and from the Mayor pane you:

1. Describe a new feature in English. The Mayor files a `draft` ticket.
2. Wait ~a minute. The Architect picks it up, investigates the code, writes a spec file in `.company_town/ticket_specs/`, and moves it to `open`.
3. The daemon assigns an idle prole (or spins up a new one up to `max_proles`). The prole builds on `prole/<name>/<id>`, pushes frequently, and files a PR.
4. The Reviewer sees the new PR, diffs it against the spec, and either approves it → `pr_open` or requests changes → `repairing`.
5. You review on GitHub. If you merge, the daemon notices the merge and closes the ticket. If you leave a review comment, the daemon moves it to `repairing` and the prole fixes it.

**As an operator of Company Town itself** — from any shell, not from tmux:

- `ct dashboard` — live TUI of agents and tickets
- `ct attach <agent>` — jump into any agent's tmux pane
- `ct architect` / `ct architect stop` — start/stop the architect manually
- `ct artisan backend` / `ct artisan backend stop` — same for a specialty artisan

## CLI reference

### `ct` (user-facing)

| Command | Action |
|---|---|
| `ct init` | Set up `.company_town/`, start Dolt, run migrations. Idempotent. Always refreshes CLAUDE.md templates from the embedded copies. |
| `ct start` | Start daemon + Mayor, attach to the Mayor's tmux session. |
| `ct stop [--clean]` | Graceful shutdown. Agents write handoffs and exit. `--clean` also prunes prole worktrees. |
| `ct nuke` | Kill every session immediately. No handoffs. |
| `ct architect [stop]` | Start or gracefully stop the Architect. |
| `ct artisan <specialty> [stop]` | Start or stop an Artisan of the given specialty. |
| `ct attach <agent>` | Attach to a running agent's tmux session. |
| `ct dashboard` | Open the live agents + tickets TUI. |
| `ct daemon` | Run the daemon only. Normally invoked by `ct start`, not by hand. |

### `gt` (agent-facing)

Agents use this directly. Users generally don't, but it's fine for debugging and one-off corrections.

| Command | Action |
|---|---|
| `gt ticket create <title> [--type task\|bug\|epic] [--parent <id>] [--specialty <s>] [--description <d>] [--priority <P0-P3>]` | Create a ticket in `draft` status. |
| `gt ticket show <id>` | Print one ticket's details (including description). |
| `gt ticket list [--status <status>]` | List tickets, optionally filtered. |
| `gt ticket ready` | List unblocked open tickets in selection order. |
| `gt ticket assign <id> <agent>` | Assign a ticket to an agent (also moves status to `in_progress`). |
| `gt ticket status <id> <status>` | Transition a ticket. |
| `gt ticket depend <id> <depends_on_id>` | Add a dependency edge. |
| `gt ticket close <id>` | Close a ticket. |
| `gt ticket delete <id>` | Hard delete. For mistakes. |
| `gt prole create <name>` | Spin up a new prole (worktree + tmux + DB row). |
| `gt prole reset <name>` | Reset an idle prole's worktree — pulls main, clears context. |
| `gt prole list` | List registered proles. |
| `gt agent register <name> <type> [--specialty <s>]` | Register a new agent in the DB. |
| `gt agent status <name> <idle\|working\|dead>` | Update an agent's status. |
| `gt pr create <ticket_id>` | File a PR for a ticket's branch. |
| `gt start <agent>` / `gt stop <agent>` | Start or stop an agent's tmux session. |
| `gt status` | Print system status. |
| `gt check <run\|list\|history>` | Run and view quality checks. |
| `gt migrate` | Apply pending database migrations. |

## Project layout

```
<your-project>/
├── .company_town/              # gitignored, all runtime state
│   ├── config.json             # the only config file you edit
│   ├── dolt-data/              # Dolt server data + config
│   ├── logs/                   # flat-file logs per agent
│   │   ├── daemon.log
│   │   ├── mayor.log
│   │   ├── architect.log
│   │   └── prole-<name>.log
│   ├── ticket_specs/           # architect writes specs here as markdown
│   ├── agents/                 # per-agent CLAUDE.md and memory/
│   │   ├── mayor/
│   │   ├── architect/
│   │   │   ├── CLAUDE.md
│   │   │   └── memory/
│   │   │       ├── handoff.md
│   │   │       └── lessons_learned.md
│   │   └── ... (reviewer, artisan/<specialty>)
│   └── proles/                 # per-prole git worktrees
│       ├── copper/
│       └── iron/
```

Everything under `.company_town/` is gitignored by convention. The CLAUDE.md templates shipped with the binary are written into `agents/` on every `ct init`, which always refreshes them from the embedded copies.

## Database

Dolt (MySQL-compatible) running as a local server on the port in `config.json`. One database per project.

Tables:
- `issues` — the ticket graph (id, type, status, title, description, priority, branch, pr_number, assignee, parent_id, timestamps)
- `agents` — agent registry (name, type, specialty, status, current_issue, tmux_session, worktree_path)
- `issue_dependencies` — dependency edges (issue_id, depends_on_id)

Migrations are embedded in `internal/db/migrations/` and run automatically on `ct init` and `gt migrate`. Because Dolt has git-like history, you can `dolt log` / `dolt diff` your ticket state machine during debugging.

## Configuration

`.company_town/config.json` is the single config file. The important fields:

| Field | Purpose |
|---|---|
| `ticket_prefix` | Used in branch and PR titles (e.g. `nc` → `[nc-42]`, `prole/iron/nc-42`). |
| `github_repo` | SSH or HTTPS URL of the upstream repo. Proles push here. |
| `dolt.host` / `dolt.port` / `dolt.database` | Local Dolt server location. |
| `max_proles` | Hard cap on concurrent proles. The daemon respects this. |
| `agents.<role>.model` | Claude model per role (`opus`, `sonnet`, or a full model id). |
| `polling_interval_seconds` | Daemon tick interval. Default 30. |
| `nudge_cooldown_seconds` | Minimum interval between nudges to the same agent. |
| `context_handoff_threshold` | Fraction (0–1) of context before long-lived agents write a handoff and exit. |
| `stuck_agent_threshold_seconds` | How long before a non-progressing prole is flagged. |
| `quality.checks` | Baseline commands the daemon runs periodically (build / test / vet by default). |

## Operations

### Starting and stopping

- `ct start` — boots the daemon, brings up the Mayor, attaches you. Safe to re-run; idempotent for already-running components.
- `ct stop` — tells every long-lived agent to write a handoff file and exit. Proles finish their current push first.
- `ct nuke` — kills every tmux session immediately. Use when things are wedged.

### Releasing a new version

Tag and push — GitHub Actions runs goreleaser automatically:

```bash
git tag v0.1.0
git push --tags
```

This produces a GitHub Release with `.tar.gz` archives for macOS (arm64, amd64) and Linux (amd64), plus a `checksums.txt`.

### Dashboard

```
ct dashboard
```

Split-pane TUI: agents on one side, tickets on the other. Keyboard nav, selection, restart/kill/nudge actions on agents, open-PR and status-change actions on tickets.

### Handoffs and memory

Long-lived agents (mayor, architect, artisans) write a `handoff.md` to `.company_town/agents/<type>/memory/` when they hit `context_handoff_threshold` or are asked to stop gracefully. The next session of that agent reads the file on start and resumes. Agents also write their own `lessons_learned.md` in the same directory.

### Logs

Everything is flat files under `.company_town/logs/`. No log state in the database. Structured lines where possible (`[TIMESTAMP] [AGENT] [LEVEL] message`). Tail `daemon.log` when troubleshooting pickup, assignment, or PR events.

### Quality checks

The daemon runs the commands in `config.json > quality.checks` on the `baseline_interval_seconds` cadence and records pass/fail history. View with:

```bash
gt check list
gt check history build --limit 20
gt check run
```

## Rules of the road

- **Never push to `main`.** All work lives on `prole/<name>/<id>` branches. Humans merge PRs.
- **Proles are sonnet. Architect, Mayor, Artisans are opus.** Override in `config.json` per role if you want.
- **Reviewer approval is not merge.** The reviewer's `pr_open` transition means *ready for human review*.
- **Human PR comments are the only signal that triggers `repairing`.** Reviewer comments are filtered by a sentinel prefix so they don't loop.
- **Dolt gives you history.** Use `dolt log issues` when state transitions look wrong.

## Further reading

- `CLAUDE.md` — build/test instructions for anyone (human or AI) hacking on Company Town itself.
- `skills/` — repeatable procedures that don't fit the ticket lifecycle. Each subdirectory is a skill bundle invocable by Claude Code. Current skills: `cut-release` (tag, goreleaser, smoke-test) and `drift-cleanup` (diagnose and fix agent/ticket state inconsistencies).
- `.company_town/ticket_specs/` — living design documents for in-progress work.
- Agent templates at `internal/commands/templates/*-CLAUDE.md` — the exact instructions each agent role receives on spawn. These are the source of truth for agent behavior.
