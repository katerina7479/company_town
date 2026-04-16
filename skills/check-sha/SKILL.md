---
name: check-sha
description: Check if the PR SHA is new or a repeated submission — escalate on loop, proceed on new SHA
---

Before reviewing a PR, verify you are not re-reviewing the same commit you already rejected. The nc-129 failure involved three rejections of SHA `4be0291` — the reviewer missed that the prole was resubmitting without changes. This skill makes that check a reflex.

## Step 1 — Get the current PR/MR head SHA

```bash
# GitHub:
gh pr view <pr_number> --json headRefOid --jq '.headRefOid'

# GitLab (mr_iid from gt ticket show):
glab mr view <mr_iid> --output json | jq -r '.sha'
```

Record this SHA.

## Step 2 — Search prior review comments for that SHA

```bash
# GitHub:
gh pr view <pr_number> --json reviews --jq '.reviews[].body' | grep -i "ct-reviewer"

# GitLab:
glab mr note list <mr_iid> | grep -i "ct-reviewer"
```

Scan the output for any prior `[ct-reviewer]` comment that mentions the same SHA. The format used in LGTM comments is `LGTM at <sha>` and in rejection comments typically appears as `at <sha>` or explicitly in the body.

## Step 3 — Evaluate

**If no prior `[ct-reviewer]` comment exists for this PR**: this is a first review. Proceed normally with `/claim-review`.

**If a prior `[ct-reviewer] LGTM` comment exists at this SHA**: the PR was approved but the ticket status is inconsistent. Do not re-review. Run:

```bash
gt ticket show <ticket-id>
```

Check the ticket status. If it is back in `in_review` despite a prior LGTM, escalate to the Mayor — this indicates a status tracking bug, not a genuine re-review request.

**If a prior `[ct-reviewer] Changes requested` comment exists at this SHA**: the prole resubmitted the same commit without making changes. Do not re-review. Post a comment and escalate:

```bash
# GitHub:
gh pr review <pr_number> --comment --body-file /dev/stdin << 'EOF'
[ct-reviewer] Same SHA as prior rejection (<sha> on <date>). No new commits detected. Escalating to Mayor — prole may be stuck or the branch was not updated.
EOF

# GitLab:
echo "[ct-reviewer] Same SHA as prior rejection (<sha> on <date>). No new commits detected. Escalating to Mayor — prole may be stuck or the branch was not updated." | glab mr note create <mr_iid> -f /dev/stdin
gt agent status reviewer idle
```

Then notify the Mayor via `gt ticket create` or a direct status note.

**If a prior `[ct-reviewer]` comment exists but at a DIFFERENT SHA**: the prole pushed new commits. This is a genuine re-review. Proceed with `/claim-review` from Step 2 (you can skip re-running tests from scratch if the diff from the prior SHA is small — use `git diff <old-sha> <new-sha>` to scope the delta).

## When to run this skill

Run `/check-sha` at the start of every patrol iteration, before `/claim-review`. It adds ~30 seconds and prevents a full re-review of unchanged code — which cost multiple round trips in the nc-129 session.
