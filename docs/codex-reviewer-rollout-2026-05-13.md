# Codex Reviewer Rollout — 2026-05-13

## Setup

- Codex CLI version: not executed (see note below)
- Model id used: N/A — code-audit only
- Test project / repo: Company Town itself (code inspection of current main)
- nc-308 children landed: nc-309 (#357 ✓), nc-310 (#370 ✓), nc-311 (#359 ✓)

**Note on live smoke:** A live end-to-end smoke requires `codex` CLI installed and
authenticated on the operator's machine, plus a running Company Town project with an
idle prole and reviewable ticket. This report covers a code audit of the complete
reviewer-on-Codex path from config to template to launch to verdict. Steps that require
live Codex execution are marked `[needs live run]`.

---

## Code audit — lifecycle node by node

### 1. Config: `runner: "codex"` in config.json

**Status: ✓ implemented (nc-309)**

`internal/config/config.go` defines `AgentConfig.Runner string`. `ct doctor` verifies
the configured runner's CLI is on PATH via the runner check added in nc-309.

### 2. Runner factory: `runner.New("codex")` → CodexRunner

**Status: ✓ implemented (nc-310)**

Both `ClaudeRunner` and `CodexRunner` live in `internal/runner/runner.go`. `CodexRunner`
mirrors `ClaudeRunner`'s three-method shape (`Command`, `ProvisionSettings`, `SettingsPath`).

- **`Command()`** builds: `codex --approval-policy full-auto --model '<model>' [prompt]`.
  No session-name arg (Codex has no equivalent of `--name`); no `--no-project-doc` flag.
- **`ProvisionSettings()`** creates `<agentDir>/.codex/config.json` (JSON, not YAML;
  in the agent's working directory, not the operator's home). Content: `{"approvalPolicy": "full-auto"}`.
  Codex picks this up automatically from CWD — no explicit `--config` flag is needed.
- **`SettingsPath()`** returns `<agentDir>/.codex/config.json`.

### 3. Agent spawn: reviewer launched with CodexRunner

**Status: ✓ plumbed (nc-309/nc-310)**

`session.AgentSessionConfig.Runner` carries the runner through from config to
`session.SpawnAttach`. The reviewer session is started with the CodexRunner's command
when `runner == "codex"`.

### 4. Template: AGENTS.md deployed instead of CLAUDE.md

**Status: ✓ implemented (nc-311)**

`commands.WriteAgentInstructions(dir, agentType, runnerName)` deploys
`templates/codex/reviewer.md` as `AGENTS.md` when `runnerName == "codex"`, vs.
`templates/claude/reviewer.md` as `CLAUDE.md` for the Claude runner.

### 5. Reviewer claims ticket: `gt agent accept <id>` → `in_review → under_review`

**Status: ✓ template fixed in this PR**

The codex reviewer template previously used `gt ticket status <id> under_review` (a
bare status transition that did not set agent status to `working`). Fixed to
`gt agent accept <id>`, which fires the workflow.accept TicketTransition
(`in_review → under_review`) and sets the agent to `working --issue <id>` in one step.
Consistent with the Claude reviewer template updated in nc-328. `[needs live run]`

### 6. Reviewer reviews diff and posts verdict

**Status: template complete — `[needs live run]`**

AGENTS.md contains the full review checklist, blocker-vs-follow-up calibration, review
comment format, and `[ct-reviewer]` / `[ct-reviewer][changes-requested]` sentinel
requirements. The sentinel format is identical to the Claude reviewer template — the
daemon's detection logic is platform-agnostic and does not care which runner posted the
comment.

### 7. Daemon promotes verdict: `pr_open` or `repairing`

**Status: ✓ daemon logic unchanged — `[needs live run]`**

`GetReviewCommentsRaw` and the daemon's review-comment detection path do not inspect
the runner. The `[ct-reviewer]` / `[ct-reviewer][changes-requested]` sentinels are the
only gate. No Codex-specific changes needed.

### 8. Mayor and architect unchanged

**Status: ✓ by construction**

`runner` defaults to `""` (empty, treated as `"claude"`) for all roles. Setting only
`agents.reviewer.runner = "codex"` leaves mayor and architect on Claude. Confirmed by
inspection of `config.go` defaults.

---

## What worked

- nc-309/310/311 prerequisite chain fully landed on main.
- Config → runner factory → session spawn path is complete and Codex-plumbed.
- AGENTS.md template deploys correctly for `runner: "codex"` agents.
- Template alignment fix (patrol loop claim step) landed in this PR.
- Daemon verdict detection is runner-agnostic — no changes needed there.
- README now documents the user-facing config flip with a concrete example.

## What broke or surprised

1. **Codex reviewer template used bare `gt ticket status` instead of `gt agent accept`.**
   The Codex template (nc-311's port from the Claude template) preserved the pre-nc-327
   claim step. After nc-327 restored `under_review` as a distinct status and nc-328
   updated the Claude reviewer template to use `gt agent accept`, the Codex template
   diverged. Fixed in this PR.

2. **No live Codex execution possible from prole worktree.** Running a live reviewer
   smoke requires the Codex CLI installed, authenticated, and a running Company Town
   project in the right state. This is a CEO/operator step, not something a prole
   can do in its isolated worktree. The code audit above covers everything that can be
   verified statically.

## Quick fixes landed in this PR

- `internal/commands/templates/codex/reviewer.md`: replaced `gt agent status reviewer working --issue <id>` + `gt ticket status <id> under_review` (two-step claim) with `gt agent accept <id>` (single step); added `gt agent release` to Key Commands and Available Commands.
- `README.md`: added "Using Codex (or another runner) for an agent" section with config example and `ct doctor` verification step.

## New children filed under nc-308

None — no blockers surfaced by the code audit.

## Open questions for CEO

1. **Live smoke still needed.** The code path is correct per audit, but a live
   `codex --model <id>` reviewer run through a real ticket lifecycle should be done
   before treating nc-308 as fully delivered. Suggest: set `agents.reviewer.runner =
   "codex"` in a test project, file a trivial ticket, and confirm the Codex reviewer
   claims, reviews, and posts a verdict with the `[ct-reviewer]` sentinel intact.

2. **Codex model id.** The config example in the README uses `<codex-model-id>` as a
   placeholder. What model id should the docs recommend? (e.g., `codex-mini-latest` or
   the full `o4-mini` handle that Codex CLI accepts.)

3. **Default reviewer runner.** The spec explicitly defers the decision to flip
   `ct init`'s default reviewer runner to Codex — that's a CEO call. This PR just
   proves the path works. File a follow-up ticket if/when that default flip is wanted.
