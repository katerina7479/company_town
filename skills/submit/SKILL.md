---
name: submit
description: End-of-ticket: lint pre-flight, push, PR create, ci_running only after push confirms, verify
---

End-of-ticket submission sequence. Follow the steps in order. Do not flip to `ci_running` before the PR exists. Do not create the PR before the push lands.

## Step 1 — Pre-flight local checks

```bash
go vet ./...
go test ./...
gofmt -l .
```

If any check fails, **stop here**. Fix the failures before continuing. Do not create a PR with known local failures — this is what caused NC-129's round trip.

`gofmt -l .` prints files with formatting issues. If any are listed, run `gofmt -w <file>` on each one.

## Step 2 — Commit any outstanding work

```bash
git status
```

If dirty, stage and commit. Never use `git add -A` — stage specific files by name:

```bash
git add <files>
git commit -m "<type>: <description> (TICKET-ID)"
```

If clean, skip this step.

## Step 3 — Push

```bash
git push origin HEAD
```

Capture the result. If the push is rejected (non-fast-forward, auth failure, network error), **do not proceed to Step 4**. Fix the push first — force-with-lease if you need to rebase, or investigate the rejection reason.

## Step 4 — Verify the push landed

```bash
git log --oneline origin/$(git branch --show-current) -1
```

The output must show your most recent commit SHA and message. If it shows an older commit, the push did not actually update the remote — debug before moving on.

## Step 5 — Create the PR

```bash
gt pr create <ticket-id>
```

This creates the PR and moves the ticket to `ci_running`. Note the PR number from the output.

## Step 6 — Verify the PR/MR

```bash
# GitHub:
gh pr view --json headRefName,state,url
# GitLab (mr_iid from gt ticket show):
glab mr view <mr_iid> --output json | jq '{source_branch: .source_branch, state: .state, web_url: .web_url}'
```

Confirm:
- `headRefName` / `source_branch` matches your local branch name
- `state` is `OPEN` (GitHub) or `opened` (GitLab)

If anything looks wrong, investigate before proceeding.

## Step 7 — Go idle

```bash
gt agent status <your-name> idle
```

**Key ordering invariant:** Steps 1 → 2 → 3 → 4 → 5 → 6 → 7. Never flip to `ci_running` before the PR exists. Never create the PR before the push is verified.
