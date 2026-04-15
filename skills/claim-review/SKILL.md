---
name: claim-review
description: Claim a ticket for review: set status, check stale base, run tests, report PR SHA and files touched
---

Batch setup for beginning a code review. Bakes in the stale-base check as a reflex — missing this manually caused a round trip in nc-130. Follow the steps in order.

## Step 1 — Claim the ticket

```bash
gt agent status reviewer working --issue <ticket-id>
gt ticket status <ticket-id> under_review
```

`under_review` leaves the prole as ticket assignee — only your agent status flips to `working`.

## Step 2 — Get the PR details

```bash
gt ticket show <ticket-id>
```

Note the `pr_number` field. Then:

```bash
gh pr view <pr_number> --json headRefOid,baseRefName,headRefName,url,title
```

Record the `headRefOid` (the current PR SHA). You will use this in Step 3 and when writing the verdict.

## Step 3 — Check for stale base

```bash
git fetch origin main
git merge-base origin/main <headRefOid>
```

Compare the merge-base SHA to the current `origin/main` HEAD:

```bash
git rev-parse origin/main
```

If the merge-base is not equal to `origin/main` HEAD, the PR branch is behind main. Note this gap — it is not necessarily a blocker, but note it in your review if the divergence is large enough to affect correctness. A gap of more than ~20 commits warrants a note to rebase before merge.

## Step 4 — List files touched

```bash
gh pr view <pr_number> --json files --jq '.files[].path'
```

Cross-reference against the ticket spec's "Affected Files" section (if present). Files in the diff but not in the spec, or in the spec but not in the diff, are worth calling out.

## Step 5 — Run the tests in the PR inspection worktree

First fetch the PR branch so the local ref is up to date:

```bash
git fetch origin <headRefName>
```

Then set up a dedicated inspection worktree and cd into it:

```bash
worktree_path=$(ct reviewer inspect <pr_number>)
cd "$worktree_path"
go test ./...
go vet ./...
```

If any tests fail, note them now. Do not skip this step — discovering test failures during PR review is the nc-129 failure mode.

The inspection worktree is cleaned up automatically in Step 5 of `/verdict`. Do NOT run `ct reviewer inspect --clean` here — leave it for the verdict step so the worktree stays available while you write your review.

## Step 6 — Report

Before writing the review, summarize to yourself:

- PR SHA: `<headRefOid>`
- Merge-base gap: `<n commits behind main>` (or "up to date")
- Files touched: `<list>`
- Test result: pass / fail (with failure details if any)
- Spec path: `.company_town/ticket_specs/<TICKET-ID>.md` (if it exists)

This summary is the foundation for your review. Proceed to read the full diff:

```bash
gh pr view <pr_number> --diff
```

Then write your verdict with `/verdict`.
