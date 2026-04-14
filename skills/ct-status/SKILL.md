---
name: ct-status
description: Fast situational read — tickets in review pipeline + agent status
---

Batch status check. Replaces four separate `gt` invocations with one pass. Use at the start of a patrol cycle for a quick read before deciding what to pick up.

## Steps

```bash
gt ticket list --status in_review
gt ticket list --status under_review
gt status
```

## What to look for

**`in_review` tickets**: these are waiting for you. Pick up the first one. If there are multiple, note the ticket IDs — you handle one at a time (pick up the next after completing the current verdict).

**`under_review` tickets**: these are actively being reviewed (usually by you in a prior iteration). If you see one here and you are currently idle, something went wrong — a prior review iteration may have left the ticket stuck. Check:

```bash
gt ticket show <id>
```

If the ticket has been `under_review` for more than one patrol cycle with no recent `[ct-reviewer]` comment, reset it:

```bash
gt ticket status <id> in_review
```

This puts it back in the queue for a fresh review.

**Agent status**: look for any agent showing `working` with no current issue, or an agent that has been `working` for an unusually long time. These are drift signals — surface them in your handoff.

## When to use

- At the start of each patrol cycle, before checking for `in_review` tickets
- After waking from a sleep interval, to get a fast situational read before deciding next action
- Before writing a handoff, to capture the current queue state
