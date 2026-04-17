---
name: verdict
description: Post review comment, transition ticket status, flip reviewer idle, clean up review worktree
---

Atomic verdict submission. Prevents the failure mode of leaving the ticket in `under_review`, the reviewer stuck as `working`, or a stale checkout orphaned in `/tmp`. Follow the steps in order.

## Preconditions

- You have completed the review and know whether you are approving or requesting changes.
- You have the PR number and ticket ID.
- Your review body is ready (draft it before running this skill — do not compose it inline in the command).

## Step 1 — File any follow-ups you noticed

Before writing the review body, scan your review notes for anything **out of scope** — neighbouring dead code, thin tests for adjacent behaviour, TODO/FIXME/XXX markers, a small refactor that would make the next ticket easier, a bug one file over. File each as its own ticket now so you can cite it in the review body.

```bash
gt ticket create "<short imperative title>" \
  --type <bug|refactor|task> \
  --parent <reviewing-ticket-id> \
  --priority <P2|P3> \
  --description "Noticed while reviewing <PREFIX-ID>. <What + where + why>. Files: path/to/file.go:LINE."
```

See the **File Follow-Ups** section of your CLAUDE.md for the full guidance on when to file vs. block. If you filed nothing, that's fine — but pause and actually ask yourself before moving on. The architect would rather triage five mediocre follow-ups than miss one good one.

Record the IDs of any filed follow-ups (e.g. `NC-201`, `NC-202`); you will cite them in Step 2.

## Step 2 — Write the review body to a temp file

```bash
cat > /tmp/review-<pr_number>.md << 'EOF'
[ct-reviewer] <your review body here>
EOF
```

**CRITICAL**: The body must start with `[ct-reviewer]`. The daemon uses this sentinel to distinguish your comments from human feedback. A missing prefix will cause your own LGTM to bounce the ticket to repairing.

For an approval (any platform):
```
[ct-reviewer] LGTM at <sha>. <any merge-relevant notes>
```

For changes requested on **GitHub**:
```
[ct-reviewer] Changes requested.

- `path/to/file.go:42` — <one-line fix required>
- `path/to/other.go:17` — <one-line fix required>

[non-blocking] Filed NC-201 for the missing prole_test.go edge case.
[non-blocking] Filed NC-202 for the TODO on dashboard.go:442.
```

For changes requested on **GitLab**: the `[changes-requested]` sentinel must immediately follow `[ct-reviewer]` so the GitLab adapter classifies the note as CHANGES_REQUESTED:
```
[ct-reviewer][changes-requested] Changes requested.

- `path/to/file.go:42` — <one-line fix required>
- `path/to/other.go:17` — <one-line fix required>

[non-blocking] Filed NC-201 for the missing prole_test.go edge case.
```

**Why both prefixes?** `[ct-reviewer]` must be first so the daemon skips the note when scanning for human feedback (it checks `HasPrefix "[ct-reviewer]"`). `[changes-requested]` must follow immediately so `GetReviewCommentsRaw` classifies the note as CHANGES_REQUESTED rather than COMMENTED. On GitLab there is no first-class request-changes review state — only this sentinel bridges the gap.

Cite every follow-up you filed in Step 1 as a `[non-blocking]` line so the PR author sees what you punted.

## Step 3 — Post the review comment

Use the CLI for this project's VCS platform:

```bash
# GitHub:
gh pr review <pr_number> --comment --body-file /tmp/review-<pr_number>.md

# GitLab (mr_iid from gt ticket show):
glab mr note create <mr_iid> --file /tmp/review-<mr_iid>.md
```

Using `--body-file` / `--file` avoids shell quoting issues. Do NOT use `-b '...'` with inline content — single-quote escaping of complex bodies is error-prone.

Verify the comment posted:

```bash
# GitHub:
gh pr view <pr_number> --comments
# GitLab:
glab mr note list <mr_iid>
```

The last comment should show your review body.

## Step 4 — Submit the ticket verdict

**If approving:**

```bash
gt ticket review <ticket-id> approve
```

This moves the ticket to `pr_open`.

**If requesting changes:**

```bash
gt ticket review <ticket-id> request-changes
```

This moves the ticket to `repairing` and notifies the prole.

## Step 5 — Verify the ticket transitioned

```bash
gt ticket show <ticket-id>
```

Header must show `[pr_open]` (approve) or `[repairing]` (request-changes). If the status is still `under_review`, the transition failed — fix it before going idle.

## Step 6 — Clean up

Remove the temp review file and the PR inspection worktree:

```bash
rm -f /tmp/review-<pr_number>.md
ct reviewer inspect --clean
```

`ct reviewer inspect --clean` is idempotent — safe to run even if the worktree
was already cleaned up or `/claim-review` exited early.

## Step 7 — Go idle

```bash
gt agent status reviewer idle
```

**Key ordering invariant:** File follow-ups → write body → post comment → verify comment → submit verdict → verify ticket → clean up → idle. Never go idle before the ticket status is confirmed. Never leave a `working` reviewer status behind.
