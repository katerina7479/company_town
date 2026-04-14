#compdef ct
# zsh completion for ct (company_town user CLI)
#
# Source: top-level commands from cmd/ct/main.go
#
# Install: add contrib/ to your fpath before compinit.
#   fpath+=(/path/to/company_town/contrib)
#   autoload -U compinit && compinit

_ct() {
  local context state state_descr line
  typeset -A opt_args

  _arguments -C \
    '1:command:->cmds' \
    '*::args:->args'

  case $state in
    cmds)
      # Source: cmd/ct/main.go switch on cmd
      local cmds=(
        'init:Set up .company_town/ in project root'
        'start:Start the Mayor and attach to tmux session'
        'stop:Graceful shutdown with handoffs'
        'nuke:Immediate shutdown, no handoffs'
        'architect:Start (or stop) the Architect agent'
        'artisan:Start an Artisan agent for a given specialty'
        'attach:Attach to a running agent session'
        'dashboard:Open the live agents and tickets TUI'
        'metrics:Show system performance metrics'
        'daemon:Run the daemon (internal)'
      )
      _describe 'ct commands' cmds
      ;;

    args)
      case $line[1] in
        stop)
          # Source: cmd/ct/main.go stop handler — accepts --clean flag
          _arguments '--clean[Remove prole worktrees immediately after signalling]'
          ;;
        architect)
          # Source: cmd/ct/main.go architect handler — optional 'stop' subcommand
          _values 'architect subcommand' 'stop[Signal Architect to write handoff and exit]'
          ;;
        artisan)
          # Source: cmd/ct/main.go artisan handler — <specialty> [stop]
          if (( CURRENT == 3 )); then
            _message 'specialty name'
          elif (( CURRENT == 4 )); then
            _values 'artisan subcommand' 'stop[Signal Artisan to write handoff and exit]'
          fi
          ;;
        metrics)
          # Source: cmd/ct/main.go metrics handler — optional --since flag
          _arguments '--since[Number of days to look back]:days:'
          ;;
        attach)
          _message 'session name'
          ;;
      esac
      ;;
  esac
}

_ct "$@"
