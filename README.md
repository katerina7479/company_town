# Company Town

A local, self-hosted multi-agent development system. One instance per project, one Dolt database per project. Company Town runs a small team of AI agents with defined roles, a ticket lifecycle, isolated git worktrees per worker, and a Dolt database for state. All runtime state lives under `.company_town/` in your project — nothing leaves your machine except the PRs the agents file on GitHub.

Each agent runs in its own tmux session. You interact with one of them — the **Mayor** — through a tmux pane, and the Mayor coordinates the rest: the **Architect** specs tickets, a background **daemon** assigns them, **Proles** implement them in isolated worktrees, and the **Reviewer** AI-reviews PRs before a human merges.

## Requirements

**To run Company Town** (any install path):

- [Dolt](https://docs.dolthub.com/introduction/installation)
- tmux
- `gh` (GitHub CLI), authenticated against the repo you want agents to push to
- `claude` CLI (Claude Code), authenticated

**To build from source** (optional, contributors only):

- Go 1.22+

## Getting Started

### 1. Install the binaries

Grab the latest release from [github.com/katerina7479/company_town/releases/latest](https://github.com/katerina7479/company_town/releases/latest). Pick the archive that matches your platform:

- `company_town_<version>_darwin_arm64.tar.gz` — macOS, Apple Silicon
- `company_town_<version>_darwin_amd64.tar.gz` — macOS, Intel
- `company_town_<version>_linux_amd64.tar.gz` — Linux x86_64

Download, extract, and move the binaries onto your `PATH`:

```bash
curl -L https://github.com/katerina7479/company_town/releases/latest/download/company_town_<version>_darwin_arm64.tar.gz | tar xz
sudo mv ct gt /usr/local/bin/
```

Swap the archive name for your platform. Each release page includes a `checksums.txt` you can verify against if you care.

### 2. Verify

```bash
ct --version
gt --version
```

Both should print a version string. If you get `command not found`, the binaries aren't on your `PATH` — add the directory you moved them into to `$PATH` and retry.

### 3. Initialize a project

```bash
cd ~/your-project
ct init
$EDITOR .company_town/config.json
```

`ct init` creates `.company_town/`, starts the Dolt server, runs migrations, writes a `.gitignore` entry for `.company_town/` at the project root (so nothing Company Town tracks leaks into your repo), and drops a default `config.json`.

At minimum, set these three fields:

- `ticket_prefix` — two or three letters used in ticket IDs, branch names, and PR titles (e.g. `nc` → `nc-42`, `[nc-42] Title`, `prole/iron/nc-42`).
- `platform` — `github` or `gitlab`. Required; there is no default.
- `repo` — `owner/repo` (github) or `namespace/project` (gitlab). Proles push branches here.

### 4. Start the daemon and attach to the Mayor

```bash
ct start
```

This boots the daemon, brings up the Mayor's tmux session, and attaches you to it. You'll land in a Claude Code pane talking to the Mayor.

**What to type into the Mayor pane:** describe in plain English what you want built. For example:

> "File a ticket to add a `--json` flag to `ct dashboard` that prints the same data as stdout instead of opening the TUI."

The Mayor files a `draft` ticket, the Architect picks it up, writes a spec under `.company_town/ticket_specs/`, and moves it to `open`. The daemon assigns an idle prole (or spins a new one up to `max_proles`), the prole implements and pushes a PR, and the Reviewer diffs it against the spec. You review the final PR on GitHub and merge.

To detach from tmux without killing anything: `Ctrl-b d`. To come back later: `ct attach mayor` (or rerun `ct start` — it's idempotent). To shut down: `ct stop` (graceful — agents write handoffs) or `ct nuke` (immediate — no handoffs). Neither command drops the database; rerun `ct start` to resume.

## Install from source

Contributors and anyone tracking `main` directly can build from the repo:

```bash
git clone https://github.com/katerina7479/company_town.git
cd company_town
make install        # builds and installs ct + gt to $GOPATH/bin
```

`make build` alone drops binaries at `./bin/ct` and `./bin/gt`. Requires Go 1.22+.

## Troubleshooting first run

If something goes wrong on your first `ct start`, tail the daemon log — that's where the clearest error messages live:

```bash
tail -f .company_town/logs/daemon.log
```

Common first-run failures:

- **`tmux: command not found`** — install tmux (`brew install tmux` / `apt install tmux`).
- **`gh: not authenticated`** (github) or **`glab: not authenticated`** (gitlab) — run `gh auth login` or `glab auth login` against the repo in `repo`.
- **`claude: command not found`** — install and authenticate the Claude Code CLI.
- **Dolt port already in use** — another project's Dolt server is already bound. Either stop it or change `dolt.port` in `config.json`.
- **`ct start` exits immediately with no visible error** — check `.company_town/logs/daemon.log`; a failed migration or config parse error shows up there.

If you hit something not on this list, `.company_town/logs/` has a flat file per agent — start with `daemon.log`, then the specific agent that seems wedged.

## How it works

### Agents

| Agent | Model | Lifetime | Responsibility |
|---|---|---|---|
| **Mayor** | opus | long-lived | Human interface. Starts/stops other agents, handles escalations, receives merge notifications. |
| **Architect** | opus | long-lived | Picks up `draft` tickets, investigates the codebase, writes specs under `.company_town/ticket_specs/`, moves tickets to `open`. |
| **Daemon** | — | background | Watches `open` and `repairing` tickets and assigns them to proles. Runs automatically with `ct start`. |
| **Reviewer** | sonnet | long-lived | AI-reviews PRs when tickets hit `in_review`, posts a GitHub review, moves the ticket to `pr_open` or `repairing`. |
| **Proles** | sonnet | ephemeral | Implementation workers. One ticket at a time, one git worktree each, named after metals (`copper`, `iron`, `tin`…). Die and respawn constantly. |
| **Artisans** | opus | long-lived | Senior specialist coders (`backend`, `frontend`, `qa_coder`). Unlike proles, they keep context across tickets and handle the harder work in their specialty. |

**Prole vs. Artisan.** Proles are the default. The daemon spins one up for any ticket that needs hands; when the ticket closes, the prole's worktree is reset and it picks up the next one. An artisan is a deliberate choice you make when a domain needs continuity — e.g. a frontend artisan that carries design-system context across a week of frontend tickets rather than starting fresh each time. You start artisans explicitly with `ct artisan <specialty>`; the daemon routes tickets whose `specialty` matches to them before falling back to a generic prole.

Agents talk to the system through the internal `gt` CLI. Users talk to the Mayor, not to `gt`.

### Ticket lifecycle

```
draft ─► open ─► in_progress ─► in_review ─► under_review ─► pr_open ─► closed
  │                                │              │             │
  │                           repairing ◄───-─────┴─────────────┘ 
  ├──► on_hold
  └──► cancelled (any stage)
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
| `closed` | Daemon | PR merged (auto-detected) or manually closed. Work landed in main. |
| `cancelled` | Any | Work was abandoned, did not land. Terminal — treated like `closed` for dependency gating and epic auto-close. |
| `on_hold` | Any | Blocked by an external input. |

Epics are containers, never workable. Proles don't touch them.

### Selection order

When the daemon has idle prole slots and selectable work, it picks tickets in strict lexicographic order:

1. `repairing` before `open`
2. bugs before tasks before anything else
3. P0 → P1 → P2 → P3 → null
4. lower id first

Deterministic. No LLM is involved in the pick.

### Daemon

A background goroutine inside `ct start` that polls every 30 seconds (configurable via `polling_interval_seconds`). Each tick it:

- Restarts dead architect/reviewer sessions when they have work to do
- Nudges the architect about `draft` tickets
- Assigns idle proles to the top selectable tickets
- Detects PR merges → closes tickets
- Detects human PR review comments → moves tickets to `repairing`
- Detects dead tmux sessions → marks agents `dead`
- Runs quality baseline checks (build / test / vet) on the configured cadence

Daemon output lives at `.company_town/logs/daemon.log`.

## Daily loop

**As the user** — you never type `gt` commands directly. You run `ct start` once a day (or once a week, or whenever), and from the Mayor pane you:

1. Describe a new feature in English. The Mayor files a `draft` ticket.
2. Wait a minute. The Architect picks it up, investigates the code, writes a spec file in `.company_town/ticket_specs/`, and moves it to `open`.
3. The daemon assigns an idle prole (or spins a new one up to `max_proles`). The prole builds on `prole/<name>/<id>`, pushes frequently, and files a PR.
4. The Reviewer sees the new PR, diffs it against the spec, and either approves it → `pr_open` or requests changes → `repairing`.
5. You review on GitHub. If you merge, the daemon notices and closes the ticket. If you leave a review comment, the daemon moves it to `repairing` and the prole fixes it.

**As an operator of Company Town itself** — from any shell, not from tmux:

- `ct dashboard` — live TUI of agents and tickets
- `ct attach <agent>` — jump into any agent's tmux pane
- `ct architect` / `ct architect stop` — start/stop the architect manually
- `ct artisan backend` / `ct artisan backend stop` — same for a specialty artisan

## CLI reference

### `ct` (user-facing)

| Command | Action |
|---|---|
| `ct init` | Set up `.company_town/`, start Dolt, run migrations, write `.gitignore` entries. Idempotent. Always refreshes agent CLAUDE.md templates from the embedded copies — see the warning below. |
| `ct doctor` | Check system dependencies (`dolt`, `tmux`, `gh`, `claude`) and project setup. Run this first if `ct start` is failing. |
| `ct start` | Start daemon + Mayor, attach to the Mayor's tmux session. Idempotent. |
| `ct stop [target] [--clean]` | Graceful shutdown. With no target, stops every session. With a target (`daemon`, `architect`, `reviewer`, `artisan-<specialty>`, `prole-<name>`), stops only that one. `--clean` prunes prole worktrees after stopping; applies only to prole targets. |
| `ct nuke [target]` | Kill sessions immediately. No handoffs. With no target, kills everything. Targets: `daemon`, `architect`, `mayor`, `reviewer`, `prole-<name>`, `artisan-<specialty>`, `bare` (the shared bare clone). |
| `ct architect [stop]` | Start or gracefully stop the Architect. |
| `ct artisan <specialty> [stop]` | Start or stop an Artisan of the given specialty. |
| `ct attach <name>` | Attach to a running agent's tmux session. |
| `ct dashboard` | Split-pane TUI of agents and tickets. |
| `ct quality` | Live quality-metrics TUI dashboard (coverage, lint, todo count, etc. with sparklines). |
| `ct metrics [--since N]` | Print system performance metrics. Defaults to the last 7 days. |

> **Heads up: `ct init` overwrites agent CLAUDE.md files.** The templates under `.company_town/agents/*/CLAUDE.md` are rewritten from the embedded copies on every `ct init`. If you want to customize agent behavior, edit the source templates at `internal/commands/templates/*-CLAUDE.md` and rebuild, *not* the deployed copies — those will be clobbered on the next `ct init`.

### `gt` (agent-facing)

Agents use this directly. Users generally don't, but it's fine for debugging and one-off corrections.

**Tickets**

| Command | Action |
|---|---|
| `gt ticket create <title> [--type task\|bug\|epic] [--parent <id>] [--specialty <s>] [--description <d>] [--priority <P0-P3>]` | Create a ticket in `draft` status. |
| `gt ticket show <id>` | Print one ticket's details (including description). |
| `gt ticket list [--status <status>]` | List tickets, optionally filtered. |
| `gt ticket ready` | List unblocked `open` tickets in selection order. |
| `gt ticket assign <id> <agent>` | Set a ticket's assignee. Does **not** change status — the prole's accept workflow flips it to `in_progress` when the prole picks it up. |
| `gt ticket unassign <id>` | Clear a ticket's assignee. |
| `gt ticket status <id> <status>` | Transition a ticket's status. |
| `gt ticket type <id> <task\|bug\|epic>` | Change a ticket's type. |
| `gt ticket priority <id> <P0-P3>` | Change a ticket's priority. |
| `gt ticket close <id>` | Close a ticket. |
| `gt ticket delete <id>` | Hard delete. For mistakes. |
| `gt ticket depend <id> <depends_on_id>` | Add a dependency edge (id is blocked by depends_on_id). |
| `gt ticket undepend <id> <depends_on_id>` | Remove a dependency edge. |
| `gt ticket parent <id> <parent_id>` | Attach a ticket to a parent (epic). |
| `gt ticket unparent <id>` | Detach a ticket from its parent. |
| `gt ticket review <id> <approve\|request-changes>` | Reviewer's verdict transition — moves the ticket to `pr_open` or `repairing`. |

**Agents and proles**

| Command | Action |
|---|---|
| `gt agent register <name> <type> [--specialty <s>]` | Register a new agent row in the DB. |
| `gt agent status <name> <idle\|working\|dead> [--issue <id>]` | Update an agent's status, optionally tagging the ticket it's working on. |
| `gt agent accept <id>` | Agent claims a ticket it's been assigned. Fires the role's `accept` workflow (for proles, this flips the ticket to `in_progress`). |
| `gt agent release <id>` | Agent releases its current ticket back to the pool. |
| `gt agent do <id>` | Convenience: accept + start working in one step. |
| `gt prole create <name>` | Spin up a new prole (worktree + tmux + DB row). |
| `gt prole reset <name>` | Reset an idle prole's worktree — pulls main, clears context. |
| `gt prole list` | List registered proles. |
| `gt create reviewer <name>` | One-shot helper to register and start a Reviewer agent. |

**PRs, sessions, system**

| Command | Action |
|---|---|
| `gt pr create <ticket_id>` | File a PR for a ticket's branch. |
| `gt start <agent>` / `gt stop <agent>` | Start or stop an agent's tmux session. |
| `gt status` | Print system status. |
| `gt check <run\|list\|history>` | Run and view quality checks. |
| `gt log <tail\|show> [flags]` | Read the command audit log. `gt log show --entity <ticket-id>` is the first-stop debugger when a ticket's state looks wrong. |
| `gt migrate` | Apply pending database migrations. |

## Project layout

```
<your-project>/
├── .gitignore                  # ct init adds .company_town/ here automatically
└── .company_town/              # gitignored, all runtime state
    ├── config.json             # the only config file you edit
    ├── dolt-data/              # Dolt server data + config
    ├── logs/                   # flat-file logs per agent
    │   ├── daemon.log
    │   ├── mayor.log
    │   ├── architect.log
    │   └── prole-<name>.log
    ├── ticket_specs/           # architect writes specs here as markdown
    ├── agents/                 # per-agent CLAUDE.md and memory/
    │   ├── mayor/
    │   ├── architect/
    │   │   ├── CLAUDE.md       # overwritten by ct init — do not edit in place
    │   │   └── memory/
    │   │       ├── handoff.md
    │   │       └── lessons_learned.md
    │   └── ... (reviewer, artisan/<specialty>)
    └── proles/                 # per-prole git worktrees
        ├── copper/
        └── iron/
```

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
| `platform` | `github` or `gitlab`. Required. |
| `repo` | `owner/repo` (github) or `namespace/project` (gitlab). Proles push here. |
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

### Dashboard

```
ct dashboard
```

Split-pane TUI: agents on one side, tickets on the other. Keyboard nav, selection, restart/kill/nudge actions on agents, open-PR and status-change actions on tickets.

### Handoffs and memory

Long-lived agents (mayor, architect, artisans) write a `handoff.md` to `.company_town/agents/<type>/memory/` when they hit `context_handoff_threshold` or are asked to stop gracefully. The next session of that agent reads the file on start and resumes. Agents also write their own `lessons_learned.md` in the same directory. These memory files are **not** overwritten by `ct init` — only the CLAUDE.md template is.

### Logs

Everything is flat files under `.company_town/logs/`. No log state in the database. Structured lines where possible (`[TIMESTAMP] [AGENT] [LEVEL] message`). Tail `daemon.log` when troubleshooting pickup, assignment, or PR events.

### Quality checks

The daemon runs the commands in `config.json > quality.checks` on the `baseline_interval_seconds` cadence and records pass/fail history. View with:

```bash
gt check list
gt check history build --limit 20
gt check run
```

#### Check schema

Each entry in `quality.checks` supports:

| Field | Type | Required | Notes |
|---|---|---|---|
| `name` | string | yes | Unique identifier; used by `gt check history <name>`. |
| `command` | string | yes | Shell command the daemon runs. For `pass_fail`, exit 0 = pass. For `metric`, stdout must be a single number. |
| `type` | string | yes | `"pass_fail"` or `"metric"`. |
| `threshold` | number | metric only | Pass boundary. Value ≥ threshold for `"higher"`, ≤ threshold for `"lower"`. |
| `warn_threshold` | number | metric, optional | Warn band edge. Ignored when zero or unset. |
| `direction` | string | metric, optional | `"higher"` (default) or `"lower"` — which direction means better. |
| `enabled` | bool | yes | Whether the daemon runs this check on the baseline cadence. |

**pass_fail example** — a build check that passes when `go build ./...` exits 0:

```json
{
  "name": "build",
  "command": "go build ./...",
  "type": "pass_fail",
  "enabled": true
}
```

**metric example** — test coverage; passes at ≥ 70 %, warns between 60 % and 70 %:

```json
{
  "name": "go_test_coverage",
  "command": "go test $(go list ./...) -coverprofile=.coverage.out >/dev/null 2>&1; go tool cover -func=.coverage.out | awk '/^total:/ {gsub(\"%\",\"\"); print $3}'",
  "type": "metric",
  "threshold": 70.0,
  "warn_threshold": 60.0,
  "direction": "higher",
  "enabled": true
}
```

## Rules of the road

- **Never push to `main`.** All work lives on `prole/<name>/<id>` branches. Humans merge PRs.
- **Proles are sonnet. Architect, Mayor, Artisans are opus.** Override in `config.json` per role if you want.
- **Reviewer approval is not merge.** The reviewer's `pr_open` transition means *ready for human review*.
- **Human PR comments are the only signal that triggers `repairing`.** Reviewer comments are filtered by a sentinel prefix so they don't loop.
- **Dolt gives you history.** Use `dolt log issues` when state transitions look wrong.

## Releasing a new version

For maintainers cutting a release: tag and push — GitHub Actions runs goreleaser automatically.

```bash
git tag v0.1.0
git push --tags
```

This produces a GitHub Release with `.tar.gz` archives for macOS (arm64, amd64) and Linux (amd64), plus a `checksums.txt`. See the `cut-release` skill for the full pre-flight checklist.

## Further reading

- `CLAUDE.md` — build/test instructions for anyone (human or AI) hacking on Company Town itself.
- `skills/` — repeatable procedures that don't fit the ticket lifecycle. Each subdirectory is a skill bundle invocable by Claude Code. Current skills: `cut-release` (tag, goreleaser, smoke-test) and `drift-cleanup` (diagnose and fix agent/ticket state inconsistencies).
- `.company_town/ticket_specs/` — living design documents for in-progress work.
- Agent templates at `internal/commands/templates/*-CLAUDE.md` — the exact instructions each agent role receives on spawn. These are the source of truth for agent behavior.
