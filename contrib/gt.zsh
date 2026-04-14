#compdef gt
# zsh completion for gt (company_town agent CLI)
#
# Source: top-level commands from cmd/gt/main.go
#         subcommands from internal/gtcmd/{ticket,prole,agent,pr,check}.go
#         status values from internal/repo/issues.go (ValidStatuses)
#         type values from internal/repo/issues.go (ValidTypes)
#
# Install: add contrib/ to your fpath before compinit.
#   fpath+=(/path/to/company_town/contrib)
#   autoload -U compinit && compinit

_gt() {
  local context state state_descr line
  typeset -A opt_args

  _arguments -C \
    '1:command:->cmds' \
    '*::args:->args'

  case $state in
    cmds)
      # Source: cmd/gt/main.go switch on cmd
      local cmds=(
        'ticket:Manage tickets'
        'prole:Manage proles'
        'agent:Manage agents'
        'pr:File PRs'
        'create:Create and launch an agent (nc-96: reviewer|prole|artisan)'
        'start:Start an agent'
        'stop:Stop an agent (graceful)'
        'status:Print system status'
        'check:Run and view quality checks'
        'migrate:Apply pending database migrations'
        'log:Read the command audit log (nc-82: tail|show)'
      )
      _describe 'gt commands' cmds
      ;;

    args)
      case $line[1] in
        ticket) _gt_ticket  ;;
        prole)  _gt_prole   ;;
        agent)  _gt_agent   ;;
        pr)     _gt_pr      ;;
        check)  _gt_check   ;;
        create) _gt_create  ;;
        log)    _gt_log     ;;
      esac
      ;;
  esac
}

_gt_ticket() {
  local context state state_descr line
  typeset -A opt_args

  _arguments -C \
    '1:subcommand:->subcmds' \
    '*::args:->args'

  case $state in
    subcmds)
      # Source: internal/gtcmd/ticket.go Ticket() switch + ticketDispatch()
      local subcmds=(
        'create:Create a new ticket'
        'show:Show a ticket'
        'list:List tickets by status'
        'ready:List ready tickets'
        'assign:Assign a ticket to a prole'
        'unassign:Clear ticket assignee'
        'status:Update ticket status'
        'type:Update ticket type'
        'prioritize:Update ticket priority'
        'describe:Update ticket description'
        'close:Close a ticket'
        'delete:Delete a ticket'
        'depend:Manage ticket dependencies'
      )
      _describe 'gt ticket subcommands' subcmds
      ;;

    args)
      case $line[1] in
        create)
          # Source: internal/gtcmd/ticket.go ticketCreate()
          _arguments \
            '--type[Issue type]:type:(task bug epic refactor)' \
            '--priority[Priority]:priority:(P0 P1 P2 P3)' \
            '--parent[Parent ticket ID]:id:' \
            '--specialty[Specialty]:specialty:' \
            '--description[Description]:description:'
          ;;
        status)
          # Source: internal/repo/issues.go ValidStatuses
          if (( CURRENT == 3 )); then
            _message 'ticket id'
          else
            local statuses=(
              draft open in_progress
              in_review under_review pr_open
              reviewed repairing on_hold merge_conflict closed
            )
            compadd -a statuses
          fi
          ;;
        type)
          # Source: internal/repo/issues.go ValidTypes
          if (( CURRENT == 3 )); then
            _message 'ticket id'
          else
            compadd task bug epic refactor
          fi
          ;;
        prioritize)
          # Source: internal/gtcmd/ticket.go ticketPrioritize()
          if (( CURRENT == 3 )); then
            _message 'ticket id'
          else
            compadd P0 P1 P2 P3
          fi
          ;;
      esac
      ;;
  esac
}

_gt_prole() {
  local context state state_descr line
  typeset -A opt_args

  _arguments -C \
    '1:subcommand:->subcmds' \
    '*::args:->args'

  case $state in
    subcmds)
      # Source: internal/gtcmd/prole.go Prole() switch
      local subcmds=(
        'create:Create a new prole'
        'reset:Reset a prole'
        'list:List all proles'
      )
      _describe 'gt prole subcommands' subcmds
      ;;
  esac
}

_gt_agent() {
  local context state state_descr line
  typeset -A opt_args

  _arguments -C \
    '1:subcommand:->subcmds' \
    '*::args:->args'

  case $state in
    subcmds)
      # Source: internal/gtcmd/agent.go Agent() switch
      local subcmds=(
        'register:Register an agent'
        'status:Update agent status'
        'accept:Accept a ticket assignment'
        'release:Release the current ticket'
        'do:Run a named workflow action'
      )
      _describe 'gt agent subcommands' subcmds
      ;;

    args)
      case $line[1] in
        status)
          # Source: internal/gtcmd/agent.go agentStatus()
          if (( CURRENT == 3 )); then
            _message 'agent name'
          elif (( CURRENT == 4 )); then
            compadd idle working dead
          else
            _arguments '--issue[Issue ID]:id:'
          fi
          ;;
        register)
          # Source: internal/gtcmd/agent.go agentRegister()
          if (( CURRENT >= 4 )); then
            _arguments '--specialty[Specialty]:specialty:'
          fi
          ;;
      esac
      ;;
  esac
}

_gt_pr() {
  local context state state_descr line
  typeset -A opt_args

  _arguments -C \
    '1:subcommand:->subcmds'

  case $state in
    subcmds)
      # Source: internal/gtcmd/pr.go PR() switch
      local subcmds=(
        'create:Create a pull request for a ticket'
        'update:Push repairs and move ticket back to in_review'
      )
      _describe 'gt pr subcommands' subcmds
      ;;
  esac
}

_gt_check() {
  local context state state_descr line
  typeset -A opt_args

  _arguments -C \
    '1:subcommand:->subcmds'

  case $state in
    subcmds)
      # Source: internal/gtcmd/check.go Check() switch
      local subcmds=(
        'run:Run quality baseline checks'
        'list:List check results'
        'history:Show check history'
      )
      _describe 'gt check subcommands' subcmds
      ;;
  esac
}

_gt_create() {
  local context state state_descr line
  typeset -A opt_args

  _arguments -C \
    '1:noun:->nouns' \
    '*::args:->args'

  case $state in
    nouns)
      # Source: cmd/gt/main.go + internal/gtcmd/create.go (nc-96)
      # Currently only 'reviewer' is implemented; extend as nc-96 lands more nouns.
      local nouns=(
        'reviewer:Create and launch a Reviewer agent'
      )
      _describe 'gt create nouns' nouns
      ;;

    args)
      case $line[1] in
        reviewer)
          _message 'reviewer name'
          ;;
      esac
      ;;
  esac
}

_gt_log() {
  local context state state_descr line
  typeset -A opt_args

  _arguments -C \
    '1:subcommand:->subcmds' \
    '*::args:->args'

  case $state in
    subcmds)
      # Source: internal/gtcmd/log.go (nc-82)
      local subcmds=(
        'tail:Stream recent command log entries'
        'show:Show command log entries'
      )
      _describe 'gt log subcommands' subcmds
      ;;
  esac
}

_gt "$@"
