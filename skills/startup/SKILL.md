---
name: startup
description: Beginning-of-ticket: show spec, accept, flip ticket to in_progress, set up branch
---

Beginning-of-ticket sequence. Follow the steps in order. Do not write any code until you are verified on the correct branch.

## Step 1 — Read the ticket

```bash
gt ticket show <id>
```

Absorb the full spec, acceptance criteria, and the `branch` field. Note the ticket's current **status**:
- `repairing` → this is a repair assignment. Use the `/repair` skill instead.
- `open` or `in_progress` → this is new work. Continue below.

## Step 2 — Accept the assignment

```bash
gt agent accept <id>
```

This flips your agent row to `working` and records `current_issue=<id>`. Do this **before** any git operation so the dashboard reflects reality immediately. Skipping this step leaves you showing as idle while you work — the architect will have to prompt you to fix it.

## Step 3 — Move the ticket to in_progress (new work only)

```bash
gt ticket status <id> in_progress
```

**This step is mandatory for new work (`open` or `in_progress` tickets).** Do it immediately after `gt agent accept`, before touching any file or branch. The daemon does not auto-transition for proles — you must call this explicitly. Skipping it leaves the ticket showing as `open` while you are working, which corrupts system state.

> For `repairing` tickets: skip this step — leave the status as `repairing`.

## Step 4 — Verify both transitions landed

```bash
gt ticket show <id>
```

The header must show your agent name as assignee and status as `in_progress`. If the status is still `open`, re-run `gt ticket status <id> in_progress`. Fix it before writing code.

## Step 5 — Set up your branch

For new work, create a fresh branch from `main`:

```bash
git fetch origin main
git checkout -b <branch> origin/main
```

Use the exact branch name from the `branch` field in `gt ticket show` — do not construct it yourself.

## Step 6 — Verify you are on the right branch

```bash
git branch --show-current
```

Output must exactly match the branch name from Step 1. If it does not, stop and fix it before touching any file.

## Step 7 — Announce

One sentence describing what you are about to implement, so the pane reader can follow along. Example: "Implementing the `/submit` skill — creating SKILL.md and updating prole template."

**Key ordering invariant:** Read → accept → in_progress → verify → branch → verify branch → announce. Never write code before step 6 passes.
