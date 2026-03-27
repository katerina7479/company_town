# Company Town Workflow

## Ticket State Machine

```
draft → open → in_progress → in_review → under_review → pr_open → closed
          ↓                       ↓            ↓            ↓
       on_hold              repairing ←────────┴────────────┘
          ↑                       ↓
          └───────────────────────┘
```

## Status Ownership

| Status | Owner | Responsibility |
|--------|-------|----------------|
| `draft` | Architect | Write spec, break down epics, move to `open` when ready |
| `open` | Conductor | Assign to available prole |
| `in_progress` | Prole | Implement the spec, create PR, move to `in_review` |
| `in_review` | Reviewer | Pick up for AI review |
| `under_review` | Reviewer | Actively reviewing, then → `pr_open` (approve) or `repairing` (issues found) |
| `pr_open` | Human | Review PR on GitHub, merge → `closed`, or request changes → `repairing` |
| `repairing` | Prole | Address review comments, push fixes, move back to `in_review` |
| `closed` | Daemon | Automatic when PR merges |
| `on_hold` | Anyone | Ticket is blocked or waiting for external input |

## Agent Roles

| Agent | Type | Responsibilities |
|-------|------|------------------|
| Mayor | mayor | Human interface, escalations, project oversight |
| Architect | architect | Specs, epic breakdown, technical design |
| Conductor | conductor | Work assignment, prole management |
| Reviewer | reviewer | AI code review before human review |
| Proles | prole | Implementation, one ticket at a time |
| Artisans | artisan | Long-lived specialists (QA, docs, etc.) |

## Transitions

### Happy Path
```
Mayor creates ticket (draft)
  → Architect writes spec (draft → open)
    → Conductor assigns prole (open → in_progress)
      → Prole implements & creates PR (in_progress → in_review)
        → Reviewer reviews (in_review → under_review → pr_open)
          → Human merges (pr_open → closed)
```

### Repair Loop
```
Reviewer finds issues: under_review → repairing
Human requests changes: pr_open → repairing
Prole fixes: repairing → in_review
(cycle repeats until approved)
```

### Blocking
```
Any status → on_hold (waiting for info, blocked by dependency)
on_hold → previous status (when unblocked)
```

## Daemon Responsibilities

The daemon polls every 30 seconds and:

1. **Nudges Architect** for `draft` tickets needing specs
2. **Nudges Conductor** for `open` tickets needing assignment
3. **Nudges Conductor** for `repairing` tickets needing reassignment
4. **Nudges Reviewer** for `in_review` tickets needing review
5. **Detects PR merges** → closes tickets, frees agents
6. **Detects human comments** → moves tickets to `repairing`
7. **Detects dead sessions** → marks agents as dead

## Commands Quick Reference

```bash
# Ticket lifecycle
gt ticket create "Title" --type task|bug|epic
gt ticket status <id> <status>
gt ticket assign <id> <agent>
gt ticket close <id>

# Agent status
gt agent status <name> working --issue <id>
gt agent status <name> idle

# PR workflow
gt pr create <ticket_id>

# System overview
gt status
ct dashboard
```
