## Available Commands (Complete List)

These are the ONLY commands available. Do not use any other gt/ct/bd commands.

```
gt ticket create <title> [--parent <id>] [--specialty <s>] [--type <t>]
gt ticket show <id>
gt ticket list [--status <status>]
gt ticket assign <ticket_id> <agent_name>
gt ticket status <id> <status>
gt ticket review <id> <approve|request-changes>
gt ticket close <id>
gt ticket delete <id>
gt agent register <name> <type> [--specialty <s>]
gt agent status <name> <idle|working|dead>
gt prole create <name>
gt prole reset <name>
gt pr create <ticket_id>
gt check run
gt check list
gt check history [<check-name>] [--limit <n>]
gt status
```

## VCS Platform Commands

`gt pr create` and `gt pr update` are platform-neutral — they use the configured VCS CLI
automatically. For direct platform CLI calls (reading diffs, posting review comments, checking CI),
use the appropriate CLI for this project's platform.

**GitHub** (default) — uses `gh`:

```bash
gh pr view <pr_number> --diff                                             # Read the diff
gh pr view <pr_number> --json headRefOid,headRefName,baseRefName,url,title
gh pr view <pr_number> --json files --jq '.files[].path'                  # Files touched
gh pr view <pr_number> --comments                                         # Review comments
gh pr review <pr_number> --comment --body-file <file>                     # Post comment
gh pr checks                                                              # CI status (current PR)
gh run view <run-id> --log-failed                                         # CI failure log
```

**GitLab** — uses `glab` (MR = Merge Request; use IID from `gt ticket show`):

```bash
glab mr diff <mr_iid>                                                     # Read the diff
glab mr view <mr_iid> --output json                                       # MR metadata (headRefOid → sha, source_branch, target_branch, web_url, title)
glab mr changes <mr_iid> --output json | jq '.changes[].new_path'        # Files touched
glab mr note list <mr_iid>                                                # Review comments
glab mr note create <mr_iid> --file <file>                               # Post comment
glab ci status --branch <branch>                                          # CI status
glab ci trace <job-id>                                                    # CI job log
```
