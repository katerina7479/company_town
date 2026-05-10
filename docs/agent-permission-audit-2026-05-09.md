# Agent Permission Audit — 2026-05-09

## Purpose

Walk every `gt`/`ct` mutation verb, cross-reference each verb against
every agent CLAUDE.md template's Allowed / Forbidden mutation lists, and
flag rules that are convention-only (documented in prose but not enforced
in code). Findings drive the hardening children of nc-304.

---

## Scope

**In scope**

- All `gt` mutation subcommands (ticket, agent, pr, prole, create, start,
  stop, migrate)
- All `ct` mutation commands (init, start, stop, nuke, architect, artisan,
  reviewer, update)
- Actor list: mayor, architect, reviewer, prole, artisan
- Source files analysed:
  - `internal/gtcmd/ticket.go`, `agent.go`, `agent_workflow.go`, `pr.go`,
    `prole.go`, `create.go`, `lifecycle.go`
  - `cmd/gt/main.go`, `cmd/ct/main.go`
  - `internal/repo/status.go`, `issues.go`, `agents.go`
  - `internal/commands/templates/` — all five CLAUDE.md templates

**Out of scope**

- Daemon-internal SQL mutations (daemon runs as a separate process, not
  via `gt`/`ct`)
- VCS platform commands (`gh`, `glab`) — out-of-band from the permission
  model
- Read-only verbs (show, list, ready, status, check list/history, log
  tail/show)

---

## Complete `gt` Mutation Verb Inventory

### `gt ticket` mutations

| Verb | Effect |
|------|--------|
| `create` | Insert new issue (mayor → ideating, everyone else → draft) |
| `assign` | Set assignee on ticket; sets branch; nudges agent session |
| `unassign` | Clear assignee on ticket |
| `status` | Set ticket status (with several guards — see §Code Gates) |
| `review` | Approve or request-changes (transitions under_review tickets only) |
| `close` | Move ticket to closed; clear agent current_issue |
| `delete` | Permanently remove ticket row |
| `depend` | Add dependency edge |
| `undepend` | Remove dependency edge |
| `parent` | Set parent ticket |
| `unparent` | Clear parent ticket |
| `describe` | Update description field |
| `priority` / `prioritize` | Update priority field |
| `type` | Update issue type |
| `promote` | Advance ideating → draft (only from ideating status) |

### `gt agent` mutations

| Verb | Effect |
|------|--------|
| `register` | Insert agent row |
| `status` | Update agent status (idle / working / dead / stopped) |
| `accept` | Set agent working + current_issue + optional ticket transition |
| `release` | Set agent idle + clear current_issue + optional ticket transition |
| `do` | Fire a named workflow action (ticket transition only) |

### `gt pr` mutations

| Verb | Effect |
|------|--------|
| `create` | Push branch + create VCS PR + ticket → ci_running (or pr_open for tdd_tests draft) |
| `update` | Push branch + ticket → ci_running (only from repairing) |
| `ready` | Push branch + mark draft PR ready + ticket → ci_running |

### `gt prole` mutations

| Verb | Effect |
|------|--------|
| `create` | Create prole worktree + register agent |
| `reset` | Recreate prole worktree from origin/main |

### `gt create` mutations

| Verb | Effect |
|------|--------|
| `create reviewer <name>` | Create reviewer agent worktree + tmux session + register agent |

### `gt start / stop`

| Verb | Effect |
|------|--------|
| `start <agent>` | Create tmux session for named agent |
| `stop <agent>` | Kill named agent tmux session |

### `gt migrate`

| Verb | Effect |
|------|--------|
| `migrate` | Apply pending Dolt SQL migrations |

### `ct` mutations

| Verb | Effect |
|------|--------|
| `init` | Create `.company_town/` scaffold, initial config |
| `start` | Start Mayor + Daemon tmux sessions; run auto-migrate |
| `stop [target] [--clean]` | Graceful shutdown of target or all sessions |
| `nuke [target]` | Immediate kill of target or all sessions |
| `architect [stop]` | Start or signal-stop the architect session |
| `artisan <specialty> [stop]` | Start or signal-stop an artisan session |
| `reviewer inspect <pr>` | Create PR inspection worktree |
| `reviewer inspect --clean` | Remove PR inspection worktree |
| `update` | Download and install latest ct/gt release binaries |

---

## Code Gates (Enforced in Go)

These constraints are compiled into the binary and cannot be bypassed via
CLI flags or environment variables:

| Gate | Location | What it prevents |
|------|----------|-----------------|
| Mayor-filed tickets land in **ideating** | `ticket.go:ticketCreate` — `CT_AGENT_NAME == "mayor"` check | Architect picking up unscoped work before CEO review |
| Direct `pr_open` set via `gt ticket status` | `ticket.go:ticketStatus` | Bypassing reviewer verdict path; only `gt ticket review <id> approve` may produce this status |
| `gt ticket review` only on `under_review` tickets | `ticket.go:ticketReview` | Reviewer posting verdict on a ticket not in review |
| `gt ticket status in_progress` requires an assignee | `ticket.go:ticketStatus` | "In progress" with no agent doing the work |
| `gt ticket promote` only from `ideating` | `ticket.go:ticketPromote` | Skipping the ideating → draft gate for tickets not yet in the mayor/CEO holding area |
| `gt pr update` only from `repairing` | `pr.go:prUpdate` | Pushing a branch re-entry outside the normal repair cycle |
| `gt ticket review` verdict limited to `approve`/`request-changes` | `ticket.go:ticketReview` | Unknown verdict strings |
| `gt ticket status cancelled` blocked if ticket is already terminal | `ticket.go:ticketStatus` | Redundant cancel on already-closed/cancelled tickets |
| `gt agent status` status must be in `ValidAgentStatuses` | `agents.go:agentStatus` + `repo/status.go` | Invalid agent statuses |
| `gt ticket type` must be in `ValidTypes` | `ticket.go:ticketCreate`, `ticketType` | Unknown issue types |
| `gt ticket create --priority` must be in `ValidPriorities` | `ticket.go:ticketCreate` | Unknown priority tiers |
| `gt agent accept` requires `CT_AGENT_NAME` env var | `agent_workflow.go:agentAccept` | Anonymous agent accepting work |
| `gt agent release` requires `CT_AGENT_NAME` env var | `agent_workflow.go:agentRelease` | Anonymous agent releasing work |
| `gt agent do` requires `CT_AGENT_NAME` env var | `agent_workflow.go:agentDo` | Anonymous action dispatch |
| `gt agent status --issue` requires `status=working` | `agent.go:agentStatus` | Setting current_issue without marking working |
| Branch must exist and match before `gt pr create` / `update` / `ready` | `pr.go:assertBranchReadyForPR` | PRs from wrong branch, detached HEAD, or default branch |
| `gt pr create` requires at least one commit ahead of origin/main | `pr.go:prCreate` | Empty PRs |

---

## Verb-by-Actor Permission Table

Legend:
- **Y** — verb is explicitly listed as **allowed** in the agent's CLAUDE.md or Allowed list  
- **N** — verb is explicitly **forbidden** by the agent's CLAUDE.md  
- **C** — convention only (no code gate; the binary allows any actor to run it)  
- **—** — not mentioned (effectively C; omission equals no restriction)

> Note: there is **no per-actor identity check** in the CLI binary.
> `CT_AGENT_NAME` is logged but not used to block access.
> All "N" cells below are enforced only by prose in CLAUDE.md templates.

### `gt ticket` mutations

| Verb | Mayor | Architect | Reviewer | Prole | Artisan | Recommended Gate |
|------|-------|-----------|----------|-------|---------|-----------------|
| `create` | **Y** | **Y** | **Y** | **Y** | **Y** | None (all need it) |
| `assign` | N (C) | — (C) | — (C) | — (C) | — (C) | Restrict to mayor + daemon; proles have no legitimate use |
| `unassign` | **Y** | — (C) | — (C) | — (C) | — (C) | Restrict to mayor + daemon |
| `status` | N (partial C) | **Y** (limited subset) | **Y** (limited subset) | **Y** (limited subset) | **Y** (limited subset) | Per-actor status-transition allowlist |
| `review` | N (C) | N (C) | **Y** | N (C) | N (C) | Restrict to reviewer + daemon |
| `close` | N (C) | N (C) | — (C) | — (C) | — (C) | Restrict to daemon (merge detection) |
| `delete` | N (C) | N (C) | — (C) | — (C) | — (C) | Restrict to human operator / ct tooling only |
| `depend` | **Y** | **Y** | **Y** (follow-up tickets) | — (C) | — (C) | Restrict to mayor + architect + daemon |
| `undepend` | **Y** | **Y** | — (C) | — (C) | — (C) | Restrict to mayor + architect + daemon |
| `parent` | **Y** | **Y** | — (C) | — (C) | — (C) | Restrict to mayor + architect |
| `unparent` | **Y** | **Y** | — (C) | — (C) | — (C) | Restrict to mayor + architect |
| `describe` | **Y** | **Y** | — (C) | — (C) | — (C) | Restrict to mayor + architect |
| `priority` | **Y** | — (C) | — (C) | — (C) | — (C) | Restrict to mayor |
| `type` | **Y** | — (C) | — (C) | — (C) | — (C) | Restrict to mayor |
| `promote` | **Y** | — (C) | — (C) | — (C) | — (C) | Restrict to mayor (ideating is mayor/CEO-only) |

### `gt agent` mutations

| Verb | Mayor | Architect | Reviewer | Prole | Artisan | Recommended Gate |
|------|-------|-----------|----------|-------|---------|-----------------|
| `register` | — (C) | — (C) | — (C) | — (C) | — (C) | Restrict to ct tooling (prole create / reviewer create) |
| `status` | **Y** (idle/dead only) | **Y** (self) | **Y** (self) | **Y** (self) | **Y** (self) | Restrict `status=working` to self (CT_AGENT_NAME); restrict setting other agents to mayor only |
| `accept` | — (C) | — (C) | — (C) | **Y** | **Y** | Restrict to self (CT_AGENT_NAME) — partial enforcement already via env var |
| `release` | — (C) | — (C) | — (C) | — (C) | — (C) | Restrict to self (CT_AGENT_NAME) |
| `do` | — (C) | — (C) | — (C) | — (C) | — (C) | Restrict to self (CT_AGENT_NAME) |

### `gt pr` mutations

| Verb | Mayor | Architect | Reviewer | Prole | Artisan | Recommended Gate |
|------|-------|-----------|----------|-------|---------|-----------------|
| `create` | N (C) | N (C) | — (C) | **Y** | **Y** | Restrict to prole + artisan + architect (conflict resolution exception) |
| `update` | — (C) | N (C) | — (C) | **Y** | **Y** | Restrict to prole + artisan |
| `ready` | — (C) | — (C) | — (C) | **Y** | **Y** | Restrict to prole + artisan |

### `gt prole` mutations

| Verb | Mayor | Architect | Reviewer | Prole | Artisan | Recommended Gate |
|------|-------|-----------|----------|-------|---------|-----------------|
| `create` | **Y** | — (C) | — (C) | N (C) | — (C) | Restrict to mayor + ct tooling |
| `reset` | **Y** | — (C) | — (C) | N (C) | — (C) | Restrict to mayor + ct tooling |

### `ct` mutations

| Verb | Mayor | Architect | Reviewer | Prole | Artisan | Recommended Gate |
|------|-------|-----------|----------|-------|---------|-----------------|
| `init` | — | — | — | — | — | Human-only (not a valid `gt` command) |
| `start` | — (C) | — | — | — | — | Restrict to mayor |
| `stop` | — (C) | — | — | — | — | Restrict to mayor |
| `nuke` | — | — | — | — | — | Human-only |
| `architect [stop]` | — (C) | — | — | — | — | Restrict to mayor |
| `artisan [stop]` | — (C) | — | — | — | — | Restrict to mayor |
| `migrate` | — | — | — | — | — | Restrict to ct start / ct daemon |

---

## Convention-Only Rules (No Code Enforcement)

These are documented prohibitions that any agent could violate today:

### CRITICAL (P1) — Escalation or data integrity risk

1. **Mayor cannot `gt ticket assign`** — mayor CLAUDE.md says Forbidden, but the
   binary allows it from any caller. Mayor assigning tickets bypasses the
   daemon's fair-assignment logic and skips the branch-name write that
   `assign.Execute` does.

2. **Mayor cannot `gt ticket close`** — forbidden by convention; close is supposed
   to happen only when a PR merges (daemon detects this). Any agent can call it.

3. **Mayor cannot `gt ticket status <X>` (non-ideating-cancel)** — only `cancelled`
   on ideating tickets is allowed. Mayor could freely set any ticket to any
   non-terminal, non-pr_open status.

4. **Reviewer verdict requires `under_review` status** — *this IS code-gated*
   (`ticketReview` checks `issue.Status == StatusUnderReview`). ✓

5. **Mayor cannot `gt pr create`** — no code gate; any agent with a valid branch
   can create a PR.

6. **Prole cannot set another agent's status to `working`** — `gt agent status
   <other-name> working` is permitted by the binary. No check verifies that the
   caller's `CT_AGENT_NAME` matches the target name.

### MODERATE (P2) — Incorrect state drift

7. **`gt ticket delete`** — mayor CLAUDE.md says "never delete tickets". Reviewer
   template lists `gt ticket close` and `delete` in available commands. No code
   guard requires special authorization.

8. **Prole using `gt ticket assign`** — prole templates list it in available
   commands; nothing prevents a prole from reassigning its own ticket to a
   different agent or assigning unrelated tickets.

9. **Mayor setting an agent to `working` for a different agent** — forbidden by
   convention ("never set other agents to `working`, that's putting words in
   their mouth") but not enforced. Any caller can run `gt agent status <name>
   working`.

10. **`gt ticket status cancelled` on non-ideating tickets by the mayor** — the
    mayor CLAUDE.md says cancelled is only for ideating tickets, but the code
    allows cancelling any non-terminal ticket from any caller.

11. **`gt ticket promote` by any agent** — ideating is the mayor/CEO holding area.
    Any agent (including a prole) could promote an ideating ticket to draft,
    bypassing CEO review. Code only checks the status is ideating; no actor
    check.

12. **Architect writing application code / filing PRs** — "Never write application
    code" is pure prose. The architect has git write access and could run
    `gt pr create`.

13. **Reviewer implementing fixes** — "Never implement fixes" is prose. Reviewer
    has no technical barrier to committing code and filing a PR.

14. **`gt agent register`** — any agent can register a new agent. Should be
    restricted to `ct prole create` / `ct create reviewer` tooling paths.

### LOW (P3) — Cosmetic drift

15. **Prole/artisan using `gt ticket describe|priority|type`** — these are mayor/
    architect territory but are accessible to all agents. A prole changing the
    priority or type of its own ticket during work is a state drift risk.

16. **Prole using `gt ticket depend|undepend|parent|unparent`** — relationship
    mutations are architect territory; proles have no listed permission for
    them but can run them freely.

---

## Recommended Gates (Implementation Priority)

These are the hardening actions this audit recommends for nc-304 children.
Each is a binary code change, not a documentation update.

| Priority | Change | Mechanism |
|----------|--------|-----------|
| P1 | Block `gt agent status <name> working` when caller's `CT_AGENT_NAME` != `<name>` (unless `CT_AGENT_NAME == "mayor"`) | Check CT_AGENT_NAME in `agentStatus()` |
| P1 | Block `gt ticket review` unless `CT_AGENT_NAME` matches the reviewer role | Check CT_AGENT_NAME in `ticketReview()` |
| P1 | Block `gt ticket assign` unless `CT_AGENT_NAME` is "mayor" or unset (daemon / operator) | Check CT_AGENT_NAME in `ticketAssign()` |
| P1 | Block `gt ticket close` unless caller is daemon or operator (no `CT_AGENT_NAME` or special flag) | Check CT_AGENT_NAME in `ticketClose()` |
| P1 | Block `gt ticket promote` unless `CT_AGENT_NAME` is "mayor" or unset | Check CT_AGENT_NAME in `ticketPromote()` |
| P2 | Block `gt ticket delete` unless `CT_AGENT_NAME` is unset (human operator) | Check CT_AGENT_NAME in `ticketDelete()` |
| P2 | Block `gt pr create|update|ready` unless `CT_AGENT_NAME` is a prole, artisan, or architect (merge-conflict exception) | Check CT_AGENT_NAME type via DB lookup |
| P2 | Block `gt agent register` unless `CT_AGENT_NAME` is unset (ct tooling) | Check CT_AGENT_NAME in `agentRegister()` |
| P2 | Block `gt ticket priority|type|describe` unless `CT_AGENT_NAME` is mayor or architect | Check CT_AGENT_NAME in respective functions |
| P3 | Block `gt ticket depend|undepend|parent|unparent` from prole/artisan agents | Check CT_AGENT_NAME in respective functions |

---

## Notes on Partial Enforcement

- **`CT_AGENT_NAME` is self-reported** — the environment variable is set by
  the tmux session launch script (in `internal/session`), not by the binary
  itself. A compromised or hand-launched agent could set it to any value.
  True isolation would require a session token signed at launch time.
  Recommend treating CT_AGENT_NAME as a best-effort guard for accidental
  misuse, not a security boundary against adversarial agents.

- **`gt ticket status` transition graph** — the code accepts any `status`
  string that isn't `pr_open`. There is no enforced state machine (e.g.,
  preventing `open → closed` directly, or `ideating → ci_running`). An
  explicit transition allowlist would catch more bugs.

- **Mayor `gt ticket status cancelled`** — the code allows cancelling any
  non-terminal ticket. The convention restricts this to ideating-only. A
  targeted code guard (`if currentStatus != StatusIdeating { return err }`)
  would close this gap cleanly.

---

## Summary Counts

| Category | Count |
|----------|-------|
| Total mutation verbs audited | 28 |
| Verbs with code-level enforcement | 15 |
| Convention-only prohibitions | 16 |
| — Critical (P1) | 6 |
| — Moderate (P2) | 8 |
| — Low (P3) | 2 |
| Recommended new code gates | 10 |
