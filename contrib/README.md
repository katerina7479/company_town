# contrib/

Shell completion scripts for `gt` and `ct`.

## Installation (zsh)

Add this directory to your `fpath` before `compinit` in your `.zshrc`:

```zsh
fpath+=(/path/to/company_town/contrib)
autoload -U compinit && compinit
```

Replace `/path/to/company_town` with the actual path to your checkout, e.g.:

```zsh
fpath+=(~/Projects/company_town/contrib)
```

Alternatively, symlink the files into an existing `fpath` directory:

```zsh
ln -s ~/Projects/company_town/contrib/gt.zsh \
      /usr/local/share/zsh/site-functions/_gt
ln -s ~/Projects/company_town/contrib/ct.zsh \
      /usr/local/share/zsh/site-functions/_ct
```

After updating `.zshrc`, reload your shell or run:

```zsh
source ~/.zshrc
```

## What is completed

### gt

- `gt <TAB>` — top-level commands (ticket, prole, agent, pr, start, stop, status, check, migrate)
- `gt ticket <TAB>` — ticket subcommands (create, show, list, status, type, prioritize, …)
- `gt ticket create --<TAB>` — flags: `--type`, `--priority`, `--parent`, `--specialty`, `--description`
- `gt ticket status <id> <TAB>` — status values (open, in_progress, in_review, …)
- `gt ticket type <id> <TAB>` — type values (task, bug, epic, refactor)
- `gt ticket prioritize <id> <TAB>` — priority values (P0, P1, P2, P3)
- `gt prole <TAB>` — prole subcommands (create, reset, list)
- `gt agent <TAB>` — agent subcommands (register, status, accept, release, do)
- `gt agent status <name> <TAB>` — agent status values (idle, working, dead)
- `gt pr <TAB>` — pr subcommands (create, update)
- `gt check <TAB>` — check subcommands (run, list, history)

### ct

- `ct <TAB>` — top-level commands (init, start, stop, nuke, architect, artisan, attach, dashboard, metrics, daemon)
- `ct stop <TAB>` — `--clean` flag
- `ct architect <TAB>` — `stop` subcommand
- `ct artisan <specialty> <TAB>` — `stop` subcommand
- `ct metrics <TAB>` — `--since` flag

## Notes

Dynamic completion (ticket IDs from `gt ticket list`, agent names from the DB)
is out of scope for this version. Only static enum values and flags are completed.
