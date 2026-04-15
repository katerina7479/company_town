---
name: repair
description: Respond to a reviewer repairing bounce: fetch failure details, rebase, fix, force-push, re-review
---

Repair sequence for a PR sent back by the reviewer. Follow the steps in order. Out-of-order repair (push then rebase, or fix without reading the feedback) costs a second round trip.

## Preconditions

- Your ticket is in `repairing` status.
- You are on the ticket's existing branch (check with `git branch --show-current`). If not, run the Startup Protocol from your CLAUDE.md first — the branch already exists on `origin`.

## Step 1 — Read the reviewer's feedback

```bash
gh pr view --comments
```

Identify the blockers — they are in a bulleted list, not prose. Do not start fixing until you understand every blocker. If any point is ambiguous, escalate rather than guess.

## Step 2 — Read the CI failures

```bash
gh pr checks
```

For any failing check, view the failure log:

```bash
gh run view <run-id> --log-failed
```

Know what is failing before you start fixing. A repair that fixes the reviewer comments but ignores CI failures will be bounced again.

## Step 3 — Sync to main

```bash
git fetch origin main
git rebase origin/main
```

If the rebase has conflicts, resolve them. **Never merge main in** — always rebase. Merging creates a merge commit that muddies the branch history and makes the diff harder to review.

## Step 4 — Fix the blockers

Address the reviewer's comments one at a time. Commit each fix as its own commit with a clear message:

```bash
git add <files>
git commit -m "fix: <what was wrong and what you did about it> (TICKET-ID)"
```

Keep commits atomic — one logical change per commit.

## Step 5 — Pre-flight locally

```bash
go vet ./...
go test ./...
gofmt -l .
```

Same checks as `/submit` Step 1. Do not push a repair that still fails locally — that is exactly how a second round trip happens.

## Step 6 — Force-push

```bash
git push --force-with-lease origin HEAD
```

`--force-with-lease` refuses the push if someone else has updated the branch since your last fetch. This is the safety net. Never use bare `--force` — it silently overwrites concurrent changes.

## Step 7 — Re-submit

```bash
gt pr update <ticket-id>
```

This pushes and moves the ticket back to `ci_running`. Do **not** file a new PR — update the existing one.

## Step 8 — Verify

```bash
gt ticket show <id>
```

Header must show `[ci_running]`. Then:

```bash
gh pr view
```

Confirm your new commits appear in the PR.

**Key ordering invariant:** Read feedback → understand CI failures → rebase → fix → pre-flight → push → re-review. Each step depends on the previous one. Skipping steps is how repairs turn into second repairs.
