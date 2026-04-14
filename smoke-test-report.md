# NC-106 Smoke Test Report

**Date:** 2026-04-13  
**Smoke dir:** `/tmp/ct-smoke-1776125922`  
**Tester:** copper (prole agent)

---

## Step-by-step results

### Step 1: `git init` + remote
**Result: OK**  
`git init` and `git remote add origin https://github.com/katerina7479/ct-smoke-test.git` worked cleanly.

### Step 2: `ct init`
**Result: Ran with warnings / rough edges**

Output:
```
Initializing company town in /tmp/ct-smoke-1776125922/.company_town
  created: agents/mayor/CLAUDE.md
  ...
Connecting to Dolt server...
Running migrations...
Company Town initialized.
Next: edit .company_town/config.json, then run `ct start`
```

**Rough edges found:**

- **NC-110 (P1):** No `.gitignore` created. `git status` after init shows "nothing to commit" only because `.company_town/` isn't tracked — but there's no `.gitignore` to make that permanent. Any `git add .` would commit the entire `.company_town/` directory including config, Dolt data, logs, and agent CLAUDE.md files.
- **NC-111 (P1):** `ct init` connected to the existing main project's Dolt server on port 3307 (shared default). Two projects on the same machine share the same `company_town` database. Their agents, tickets, and state are fully intermingled. Running `gt status` in the smoke repo shows all main project agents and tickets.
- **NC-112 (P2):** Default `ticket_prefix` is `"ct"` — same string as the CLI binary. Confusing.
- **NC-113 (P2):** `github_repo` defaults to empty string with no format hint in the file. Should be `"owner/repo"` placeholder or comment.
- **NC-114 (P2):** Default model names are `claude-opus-4-5` / `claude-sonnet-4-5`. Current models are `claude-opus-4-6` / `claude-sonnet-4-6`.

### Step 3: Edit config.json
**Result: OK** — manual edits worked. Set prefix to `smk`, `github_repo` to `katerina7479/ct-smoke-test`, port to 3307.

### Step 4: `ct start`
**Result: FAILED on first attempt, then skipped**

First attempt used port 3308 (after editing to avoid conflict). `ct start` failed with:
```
error: connecting to database: dolt server not responding: dial tcp 127.0.0.1:3308: connect: connection refused
```
`ct start` does not start Dolt — it only connects. `ct init` does start Dolt if not running, but `ct start` has no equivalent logic. If you change the port after init, `ct start` fails with an unhelpful connection-refused error.

Reverted to port 3307 (sharing main project Dolt). `ct start` was not re-run to avoid disrupting the main project's Mayor session.

### Step 5: `gt ticket create --type task --priority P2 "smoke test ticket"`
**Result: OK**

```
Created smk-109: smoke test ticket
```

Prefix applied correctly. However, ticket ID is 109 (not 1) because it shares the main project's `issues` table — confirming NC-111.

### Step 6: `gt status`
**Result: Ran with warnings**

Showed all main project agents (architect, copper, daemon, iron, mayor, reviewer, tin) alongside the smoke ticket count. No filtering by project. This confirms the database isolation failure (NC-111).

Additionally noticed: `gt status` shows copper as `working → smk-106` — this is the smoke test ticket number leaking into the main project status display. The prefix-based ticket reference is correct format but the underlying issue is shared DB.

### Step 7: `gt prole create tin`
**Result: FAILED**

```
error: setting up bare repo: creating bare clone: exit status 128
remote: Repository not found.
fatal: repository 'https://github.com/katerina7479/ct-smoke-test.git/' not found
```

`gt prole create` immediately tries to clone the GitHub repo. There's no pre-flight hint that the GitHub repo must exist first. Error message is a raw `git clone` failure with no user-friendly context.

**Rough edge:** No validation that `github_repo` points to an existing remote before attempting the bare clone. A friendlier error would be: "GitHub repo `katerina7479/ct-smoke-test` not found — ensure the repo exists and `gh` is authenticated."

Workaround: used `gt ticket assign 109 copper` directly.

**Cross-contamination found:** Assigning the smoke test ticket sent a real tmux nudge message to the main project's `copper` agent session. Database sharing caused live interference with the running system.

### Step 8: `gt pr create 109`
**Result: FAILED**

```
error: src refspec HEAD does not match any
error: failed to push some refs to 'https://github.com/katerina7479/ct-smoke-test.git'
error: pushing branch: exit status 1
```

**NC-115 (P2):** Smoke repo had no commits; `git push HEAD` fails with an opaque git error. `gt pr create` should detect "no commits on current branch" and return a clear message.

Also: `gt pr create` runs `git push` in the shell's working directory, which may differ from the project root. Could cause unexpected behavior if `gt` is invoked from a different directory.

### Step 9: `ct stop`
**Result: Ran — but with critical side effect**

```
signaled: ct-architect
stopped: ct-daemon
signaled: ct-mayor
signaled: ct-prole-copper
signaled: ct-prole-iron
signaled: ct-prole-tin
signaled: ct-reviewer
```

`ct stop` in the smoke repo signaled **all agents in the shared database**, including the main project's running agents. This is a direct consequence of NC-111 (shared database). Running `ct stop` in a second project's directory is a destructive action against the first project.

---

## Summary of rough edges

| Ticket | Priority | Finding |
|--------|----------|---------|
| NC-110 | P1 | `ct init` does not create `.gitignore` — `.company_town/` will be committed |
| NC-111 | P1 | Port 3307 + database name "company_town" hardcoded → multi-project isolation failure |
| NC-112 | P2 | Default `ticket_prefix` "ct" collides with CLI binary name |
| NC-113 | P2 | `github_repo` empty with no format hint |
| NC-114 | P2 | Stale default model names (4-5 vs 4-6) |
| NC-115 | P2 | `gt pr create` unhelpful error when branch has no commits |

**Not ticketed (lower priority):**
- `gt prole create` gives raw git clone error when GitHub repo doesn't exist — should give a friendlier message
- `ct start` does not start Dolt (only `ct init` does) — unexpected given README says `ct start` starts the system
- `ct stop` from wrong project kills other project's agents (consequence of NC-111)

---

## Acceptance criteria check

- ✅ Report listing each step, outcome, and follow-up tickets filed
- ⚠️ Did not reach step 8 via the full path (`gt pr create` failed on empty branch) — reached step 8 and observed the failure mode
- ✅ Follow-up tickets NC-110 through NC-115 filed for all hardcoded-path / wrong-error / missing-hint findings
