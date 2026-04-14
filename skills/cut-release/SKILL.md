---
name: cut-release
description: Cut a versioned release of Company Town — tag, push, watch goreleaser, smoke-test the binaries.
---

Cut a new versioned release of Company Town. Follow the steps in order; do not skip the smoke test.

## Preconditions

- `main` is green in CI (check the latest workflow run on GitHub Actions before starting).
- No open PRs with known bugs are waiting to be merged.
- You are on `main` and up-to-date: `git fetch origin && git pull --ff-only origin main`.

## Step 1 — Find the current version

```bash
git fetch --tags
git tag --sort=-v:refname | head -5
```

Pick the highest tag (e.g. `v0.3.1`). Decide the next version using semver:
- Bug-only changes → patch bump (`v0.3.1` → `v0.3.2`)
- New features → minor bump (`v0.3.1` → `v0.4.0`)
- Breaking changes → major bump (`v0.3.1` → `v1.0.0`)

## Step 2 — Tag the release

```bash
git tag -a vX.Y.Z -m "vX.Y.Z"
```

Replace `vX.Y.Z` with the version decided in Step 1. Annotated tags are required — the release workflow reads the tag message.

## Step 3 — Push the tag

```bash
git push origin vX.Y.Z
```

Do NOT push with `--tags` (that pushes all local tags). Push the specific tag by name to avoid accidentally publishing stale or test tags.

## Step 4 — Watch the release workflow

Go to the GitHub Actions tab for the repository and open the workflow run triggered by the tag push. It is named **release** and is defined in `.github/workflows/release.yml`.

Wait for goreleaser to finish. The run takes roughly 2–4 minutes. A green checkmark means assets were published. A red X means something failed — read the step logs, fix the root cause, and re-read the Rollback section below before retrying.

## Step 5 — Verify release assets

On the GitHub Releases page, confirm the new release (`vX.Y.Z`) has all of the following:

- `company_town_X.Y.Z_darwin_arm64.tar.gz`
- `company_town_X.Y.Z_darwin_amd64.tar.gz`
- `company_town_X.Y.Z_linux_amd64.tar.gz`
- `checksums.txt`

If any asset is missing, the goreleaser run was partial. Check the workflow logs before proceeding.

## Step 6 — Smoke test

```bash
cd /tmp
curl -L https://github.com/katerina7479/company_town/releases/download/vX.Y.Z/company_town_X.Y.Z_darwin_arm64.tar.gz | tar xz
./ct --version
./gt --version
```

Both commands must print `vX.Y.Z` exactly. If either prints `dev`, blank, or a different version, the build embedded the wrong version string — investigate the goreleaser config before calling the release good.

## Step 7 — Post-release

If the smoke test passes, the release is done. No further action is required unless install docs reference a pinned version — update them if so.

## Rollback

**Goreleaser failed before publishing assets:** Fix the root cause in the workflow config or code. Delete the tag locally and on origin, re-tag from the fixed `main`, and push again.

```bash
git tag -d vX.Y.Z
git push origin :refs/tags/vX.Y.Z
# fix the issue, then repeat Steps 2–3
```

**A bad release shipped (binaries are wrong):** Do NOT delete the tag. Instead:
1. Delete the GitHub Release (UI → Edit → Delete) so users don't download broken assets.
2. Fix the issue on `main` and merge via normal PR.
3. Cut a new patch release (`vX.Y.(Z+1)`) following this skill from Step 1.

Re-tagging an existing tag produces confusing git history; always increment the version instead.
