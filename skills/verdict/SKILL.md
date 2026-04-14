---
name: verdict
description: Post review comment, transition ticket status, flip reviewer idle, clean up review worktree
---

Atomic verdict submission. Prevents the failure mode of leaving the ticket in `under_review`, the reviewer stuck as `working`, or a stale checkout orphaned in `/tmp`. Follow the steps in order.

## Preconditions

- You have completed the review and know whether you are approving or requesting changes.
- You have the PR number and ticket ID.
- Your review body is ready (draft it before running this skill — do not compose it inline in the command).

## Step 1 — Write the review body to a temp file

```bash
cat > /tmp/review-<pr_number>.md << 'EOF'
[ct-reviewer] <your review body here>
EOF
```

**CRITICAL**: The body must start with `[ct-reviewer]`. The daemon uses this sentinel to distinguish your comments from human feedback. A missing prefix will cause your own LGTM to bounce the ticket to repairing.

For an approval:
```
[ct-reviewer] LGTM at <sha>. <any merge-relevant notes>
```

For changes requested — use the format from your CLAUDE.md Review Comment Format section:
```
[ct-reviewer] Changes requested.

- `path/to/file.go:42` — <one-line fix required>
- `path/to/other.go:17` — <one-line fix required>

[non-blocking] <optional note, max 2>
```

## Step 2 — Post the GitHub comment

```bash
gh pr review <pr_number> --comment --body-file /tmp/review-<pr_number>.md
```

Using `--body-file` avoids shell quoting issues. Do NOT use `-b '...'` with inline content — single-quote escaping of complex bodies is error-prone.

Verify the comment posted:

```bash
gh pr view <pr_number> --comments
```

The last comment should show your review body.

## Step 3 — Submit the ticket verdict

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

## Step 4 — Verify the ticket transitioned

```bash
gt ticket show <ticket-id>
```

Header must show `[pr_open]` (approve) or `[repairing]` (request-changes). If the status is still `under_review`, the transition failed — fix it before going idle.

## Step 5 — Clean up the temp file

```bash
rm -f /tmp/review-<pr_number>.md
```

## Step 6 — Go idle

```bash
gt agent status reviewer idle
```

**Key ordering invariant:** Write body → post comment → verify comment → submit verdict → verify ticket → clean up → idle. Never go idle before the ticket status is confirmed. Never leave a `working` reviewer status behind.
