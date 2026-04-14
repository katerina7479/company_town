---
name: drift-cleanup
description: Diagnose and fix agent/ticket drift — when gt status shows an agent in an unexpected state or a drift warning appears.
---

Use this skill when `gt status` shows an agent in an unexpected state (idle but "held", working on a closed ticket, pointing at the wrong ticket), or when a drift warning block appears in `gt status` output.

## When to use

- An agent shows `working` but no matching in-progress ticket exists.
- A ticket shows `in_progress` or `repairing` but no agent is assigned or the agent is idle.
- `gt status` prints a **Drift warnings** block.
- A prole was nudged repeatedly but never claimed the ticket.

## Step 1 — Read gt status

```bash
gt status
```

If a **Drift warnings** block is present, it usually names the exact inconsistency (e.g. "iron working on nc-131 but ticket is closed"). Note every warning — each one is a separate fix.

## Step 2 — Query the source of truth

```bash
cd .company_town/dolt-data
dolt sql -q "SELECT name, status, current_issue, tmux_session FROM agents"
dolt sql -q "SELECT id, title, status, assignee FROM issues WHERE assignee IS NOT NULL AND status NOT IN ('closed','cancelled')"
```

Read both result sets side by side. An agent is drifted when any of the following is true:

- `agents.current_issue` is set but the issue row does not exist or is closed.
- `agents.status = 'working'` but no issue row has `assignee = agent.name` and an active status.
- An issue row has `assignee = agent.name` and `status = 'in_progress'` but `agents.status = 'idle'`.

## Step 3 — Check tmux reality

```bash
tmux list-sessions | grep ct-
```

Compare session names (format: `ct-<agentname>`) against the `tmux_session` column from Step 2. Mismatches mean either:

- The session died and the daemon hasn't caught it yet — the DB still thinks the agent is alive.
- The DB has a stale session name from a prior run (historical bugs: nc-95, nc-119).

## Step 4 — Apply the fix

Choose the correct fix path based on the diagnosis:

**Stale `current_issue` on an idle agent** (agent finished but DB was not updated):

```bash
gt agent status <name> idle
```

This clears both `status` and `current_issue` in the DB atomically.

**Dead tmux session that the DB thinks is alive** (session gone, agent shows `working`):

```bash
gt agent status <name> dead
```

Then restart the agent via `ct start <agent>` or wait for the daemon's `restartDeadReviewers` / `handleDeadSessions` reconciler to do it on the next tick (up to 30 seconds by default).

**Ticket assigned to an agent that never picked it up** (ticket is `in_progress` but agent is idle and `current_issue` is blank):

```bash
gt ticket status <id> open
gt agent status <name> idle
```

This returns the ticket to the pool. The daemon will reassign it on the next tick.

**Agent working on a ticket that was closed or cancelled externally:**

```bash
gt agent status <name> idle
```

The agent's `current_issue` is cleared. If the agent's tmux session is still running, attach to it and nudge it to check for a new assignment or go idle.

## Step 5 — Verify

```bash
gt status
```

Drift warnings should no longer appear. All agents should have consistent status and `current_issue` values that match the issue table.

If warnings persist after applying the fix, the root cause is in a daemon reconciler, not in the state values — the reconciler is producing drift faster than you can clear it. In that case:

1. Tail the daemon log to identify what reconciler is running: `tail -f .company_town/logs/daemon.log`
2. File a ticket describing the reconciler misbehavior.
3. Work around by stopping and restarting the affected agent manually until the ticket is resolved.

## Never

- Do not write to Dolt directly with raw SQL to fix drift. If a fix requires SQL that has no `gt` equivalent, that is a gap in the CLI — file a ticket for the missing command and work around it through the available `gt agent status` / `gt ticket status` commands.
- Do not delete agent rows from the database to "reset" them. Use `gt agent status <name> dead` and let the daemon handle restart.
