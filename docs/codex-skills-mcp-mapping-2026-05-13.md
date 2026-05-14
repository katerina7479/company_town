# Codex Skills / MCP Mapping — 2026-05-13

## Trigger

nc-308 — making Company Town agents runnable on Codex requires understanding
what functionality ports across runners. This investigation (nc-314) catalogues
every skill and MCP entry, classifies each, and determines what work (if any)
is needed.

## Method

- Listed every skill under `skills/` and `.claude/skills/`
- Searched all agent settings files and runner source for MCP entries
- Read each skill's `SKILL.md` to identify invoking agent and mechanism
- Compared the Claude (`internal/commands/templates/claude/`) and Codex
  (`internal/commands/templates/codex/`) template sets
- Classified each item as Claude-only, Portable, or Translatable

---

## Skills Catalogue

### `skills/` (10 skills)

| Skill | Invoking agent(s) | Mechanism | What it does |
|-------|-------------------|-----------|--------------|
| `check-sha` | reviewer | slash command | Checks PR head SHA against prior review comments to detect same-SHA resubmissions; escalates on loop, proceeds on new SHA |
| `claim-review` | reviewer | slash command | Batch ticket claim + stale-base check + test run at start of a review cycle |
| `ct-status` | reviewer, mayor | slash command | Fast situational read: tickets in review pipeline + agent status |
| `cut-release` | mayor | slash command | Cuts a versioned release — tags, pushes, runs goreleaser, smoke-tests binaries |
| `drift-cleanup` | mayor, operator | slash command | Diagnoses and fixes agent/ticket drift when `gt status` shows unexpected state |
| `repair` | prole | slash command | Repair sequence for a bounced PR: read feedback → rebase → fix → force-push → re-review |
| `spec` | architect | slash command | Prints the canonical ticket spec file for a given ticket ID |
| `startup` | prole | slash command | Beginning-of-ticket: read spec, accept assignment, set up branch |
| `submit` | prole | slash command | End-of-ticket: lint pre-flight, push, create PR, flip to ci_running |
| `verdict` | reviewer | slash command | Atomic verdict: post review comment, transition ticket status, clean up worktree |

### `.claude/skills/` (1 skill)

| Skill | Invoking agent(s) | Mechanism | What it does |
|-------|-------------------|-----------|--------------|
| `spec-ticket` | architect | slash command | Writes a ticket spec in the canonical house format and files it to `.company_town/ticket_specs/<id>.md` |

---

## MCP Catalogue

**No MCP servers are configured in this project.**

Searched:
- All agent settings files under `.company_town/agents/`
- `internal/runner/runner.go` — `ClaudeRunner.ProvisionSettings` writes
  `.claude/settings.json` with only a `permissions.allow` list; no `mcpServers`
  key is ever written
- `internal/runner/runner.go` — `CodexRunner.ProvisionSettings` writes
  `.codex/config.json` with only `approvalPolicy`
- `internal/commands/templates/` (both claude/ and codex/) — no MCP
  invocations in any template

---

## Mapping Table

| Item | Type | Used by | Claude support | Codex support | Decision |
|------|------|---------|----------------|---------------|----------|
| `check-sha` | skill | reviewer | yes (slash cmd) | none | **Claude-only** — Codex reviewer template has inline SHA check procedure; no port needed |
| `claim-review` | skill | reviewer | yes (slash cmd) | none | **Claude-only** — Codex reviewer template has inline claim + stale-base check; no port needed |
| `ct-status` | skill | reviewer, mayor | yes (slash cmd) | none | **Claude-only** — Codex templates have equivalent inline status commands; no port needed |
| `cut-release` | skill | mayor | yes (slash cmd) | none | **Claude-only** — mayor stays on Claude; no Codex mayor is planned or needed |
| `drift-cleanup` | skill | mayor, operator | yes (slash cmd) | none | **Claude-only** — operator skill for Claude operators; not relevant for Codex agents |
| `repair` | skill | prole | yes (slash cmd) | none | **Claude-only** — Codex prole template has inline repair procedure; no port needed |
| `spec` | skill | architect | yes (slash cmd) | none | **Claude-only** — architect stays on Claude; no Codex architect is planned |
| `startup` | skill | prole | yes (slash cmd) | none | **Claude-only** — Codex prole template has inline startup procedure; no port needed |
| `submit` | skill | prole | yes (slash cmd) | none | **Claude-only** — Codex prole template has inline submit procedure; no port needed |
| `verdict` | skill | reviewer | yes (slash cmd) | none | **Claude-only** — Codex reviewer template has inline verdict procedure; no port needed |
| `spec-ticket` | skill | architect | yes (slash cmd) | none | **Claude-only** — architect stays on Claude; no Codex architect is planned |
| MCP servers | MCP | — | n/a | n/a | **Not applicable** — no MCP servers are configured in this project |

---

## Findings

**1. (green) All skills are Claude Code-specific by mechanism, not by content.**
Every skill is invoked via the Skill tool's slash-command model (`/startup`,
`/submit`, etc.), which is unique to Claude Code. Codex has no equivalent
invocation mechanism. This is not a gap — it is expected. Codex agents run
inline procedures in their CLAUDE.md templates instead.

**2. (green) Codex templates already cover every skill's functional equivalent.**
The `internal/commands/templates/codex/` directory has a complete template set
for every agent role. Each Codex template inlines the procedural steps that
the corresponding Claude template delegates to skills. For example:
- `codex/prole.md` has inline startup, submit, and repair procedures (without
  `/startup` / `/submit` / `/repair` slash-command references)
- `codex/reviewer.md` has an inline patrol loop, claim step, verdict step,
  and SHA-check guidance (without `/claim-review` / `/verdict` slash references)

No skill has a Codex-template gap. No porting work is needed.

**3. (green) No MCP servers — no MCP mapping needed.**
The project does not configure any MCP server integrations. The absence is
consistent: both runner implementations (`ClaudeRunner`, `CodexRunner`) write
minimal settings files and make no reference to MCP. Nothing to translate.

**4. (orange) Skill READMEs do not declare their runner compatibility.**
Each `SKILL.md` is written from a Claude Code-centric perspective and does not
explicitly note that the skill is Claude-only. A Codex agent that encounters
one of these files could misinterpret it as something it should invoke. Adding
a short `## Runner compatibility` footer to each SKILL.md would make the
constraint explicit. Low urgency (Codex agents don't currently patrol the
skills/ directory), but worth documenting.

---

## Quick Fixes Landed in This PR

None. All classification findings are documentation-level; no production code
or template changes are warranted by this investigation. The runner-compat note
(Finding #4) is filed as a recommended child below.

---

## Recommended Children of nc-308

- **(proposed) nc-308/7** — Add `## Runner compatibility: Claude Code only` footer to each of the 11 skill SKILL.md files. Prevents a Codex agent from attempting to invoke `/startup` etc. One-pass mechanical edit, no code change. Priority P5.

That is the only recommended child. All other skills/MCP gaps are non-issues:
the Codex template set is complete, no MCP is configured, and mayor/architect
remain on Claude.

---

## Open Questions for CEO

None. The investigation is self-contained. If a Codex mayor or Codex architect
is later needed, the relevant skills (`cut-release`, `spec`, `spec-ticket`,
`drift-cleanup`) will need Codex-template equivalents at that time — but that
is outside the nc-308 scope (nc-308 targets the reviewer role).
