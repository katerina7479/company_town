---
name: spec
description: Print the ticket spec file for a given ticket ID
---

Print the canonical ticket spec for a ticket. Useful during review when you need to cross-reference the implementation against the original requirements without leaving your terminal.

## Usage

```
/spec <ticket-id>
```

Example: `/spec nc-42`

## Steps

```bash
gt ticket show <ticket-id>
```

This shows the full ticket body including spec, acceptance criteria, and affected files.

If a separate spec file exists (some tickets have detailed specs written by the Architect):

```bash
SPEC_PATH=".company_town/ticket_specs/$(echo <ticket-id> | tr '[:lower:]' '[:upper:]').md"
if [ -f "$SPEC_PATH" ]; then
    cat "$SPEC_PATH"
else
    echo "No separate spec file at $SPEC_PATH — full spec is in gt ticket show output above"
fi
```

## When to use

- During `/claim-review`, to cross-reference the diff against what was asked for
- When a prole's PR summary mentions features not in the ticket — verify whether they were in-scope
- When writing a `/verdict` that cites the spec: `NC-XX §Section`
