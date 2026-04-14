---
name: spec-ticket
description: Write a Company Town ticket spec in the canonical house format. Invoke as `/spec-ticket <ticket-id>` after the ticket exists in the tracker.
---

# spec-ticket

You are the architect writing a ticket spec for Company Town. Ticket specs live at `.company_town/ticket_specs/<id>.md` and must follow the canonical format below. A good spec lets a prole start coding immediately with no exploration needed.

## Preconditions

1. The ticket already exists (`gt ticket show <id>` returns it). If it doesn't, stop and tell the user to create it first — specs need an id to file under.
2. You have read the ticket title and any existing body.
3. You have looked at the affected area of the codebase. A spec written without reading the code is a guess, not a spec.

## Canonical format

Write sections in this order. Omit sections that don't apply (e.g. a pure doc change has no schema section) but never reorder.

```markdown
# <ID>: <Title>

## Context

What exists today in the affected area. What's been tried or considered. Why
this is coming up now — the triggering observation, incident, or decision.
Two to five sentences; enough to orient a prole who hasn't seen prior discussion.

## Problem

The specific failure mode or gap, concretely. Not "the system is slow" —
"`handleCIFailure` re-queries the PR on every tick even when status is
unchanged, and each query costs one GitHub API call." If there's a reproducer,
include it. If there's a user-visible symptom, state it.

## Proposal

The chosen approach in one paragraph. Name the files and functions that will
change at a high level — not full code, but enough that a prole knows where
to look. If you considered and rejected an alternative, note it in one
sentence so the next reader doesn't re-relitigate.

## Schema / migration

(Only if data model changes.) Table diffs, new columns, new enum values, and
the migration file path (`internal/db/migrations/NNNN_<name>.sql`). Specify
whether existing rows need backfill and what the default is.

## Handler / code changes

File-by-file list of what changes, at function-level intent (not full code):

- `path/to/file.go` — `FunctionName`: what it should now do differently
- `path/to/other.go` — new function `NewThing` that does X

Keep this tight. A prole will write the actual code; you're telling them
where and what, not how.

## Template / CLAUDE.md updates

(Only if agent behavior changes.) Which templates under
`internal/commands/templates/` need updating and what guidance to add or
remove. Agent-behavior changes that skip this section ship broken because
the running agents never learn the new rule.

## Test plan

What to test and what "passing" means. Unit tests for the new functions;
integration test for the end-to-end flow if the change crosses layers.
Name the test files if they already exist.

## Acceptance

Bulleted, checkable, terminal conditions. A prole should be able to read
this list and know exactly when they're done.

- [ ] <specific observable outcome>
- [ ] <specific observable outcome>
```

## Workflow

1. **Read the ticket**: `gt ticket show <id>` to confirm the id, title, and any existing body.
2. **Read the code** you'll be referencing in the Handler section. Grep for the functions you're about to name. If a function doesn't exist, say "new function `Foo` in `bar.go`" — don't leave the reader to guess.
3. **Draft the spec** following the canonical format above. Write it directly to `.company_town/ticket_specs/<id>.md`.
4. **Move the ticket** out of `draft` when the spec is complete: `gt ticket status <id> open`. If the ticket has subtasks that need to be filed first, create them before moving the parent (`gt ticket create <title> --parent <id>`).

## Anti-patterns — do not do these

- **Writing code in the spec.** Function-level intent only. Full code belongs in the PR.
- **"TODO: figure out X" sections.** If you don't know, either find out or escalate — don't paper over it.
- **Missing Acceptance section.** Without a terminal checklist the prole doesn't know when to stop.
- **Specs that don't reference any real file paths.** If you can write the whole spec without naming a single file in the repo, you haven't analyzed enough.
- **Exploring in the skill.** This skill writes a spec. It does not investigate architecture or open PRs — those are separate flows.

## Recent exemplars

Good specs to pattern-match on (read one before writing a new spec if unsure):

- `.company_town/ticket_specs/nc-130.md` — schema migration + handler changes + template updates + precedence rules
- `.company_town/ticket_specs/nc-128.md` — worktree guardrails (infrastructure change)
- `.company_town/ticket_specs/nc-133.md` — this skill's own spec (meta)
