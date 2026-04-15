package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/katerina7479/company_town/internal/cmdlog"
	"github.com/katerina7479/company_town/internal/commands"
)

// version is set at build time via -ldflags "-X main.version=<tag>".
// Falls back to "dev" for local builds.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	if cmd == "--version" || cmd == "version" {
		fmt.Println("ct version", version)
		return
	}

	// Reject unknown commands before entering log middleware.
	switch cmd {
	case "init", "start", "stop", "nuke", "architect", "artisan", "attach", "dashboard", "metrics", "daemon", "doctor", "quality":
		// valid
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}

	err := cmdlog.Run(cmdlog.FindLogPath(), "ct", cmdlog.Actor(), os.Args[1:], func() error {
		switch cmd {
		case "init":
			return commands.Init()
		case "start":
			return commands.Start()
		case "stop":
			target, clean, parseErr := parseStopArgs(args)
			if parseErr != nil {
				return parseErr
			}
			return commands.Stop(target, clean)
		case "nuke":
			target := ""
			if len(args) > 0 {
				target = args[0]
			}
			return commands.Nuke(target)
		case "architect":
			if len(args) > 0 && args[0] == "stop" {
				return commands.ArchitectStop()
			}
			return commands.Architect()
		case "artisan":
			if len(args) < 1 {
				fmt.Fprintln(os.Stderr, "usage: ct artisan <specialty> [stop]")
				os.Exit(1)
			}
			if len(args) == 1 && args[0] == "stop" {
				fmt.Fprintln(os.Stderr, "usage: ct artisan <specialty> stop")
				os.Exit(1)
			}
			specialty := args[0]
			if len(args) > 1 && args[1] == "stop" {
				return commands.ArtisanStop(specialty)
			}
			return commands.Artisan(specialty)
		case "attach":
			if len(args) < 1 {
				fmt.Fprintln(os.Stderr, "usage: ct attach <session-name>")
				os.Exit(1)
			}
			return commands.Attach(args[0])
		case "dashboard":
			return commands.Dashboard()
		case "metrics":
			return commands.Metrics(args)
		case "daemon":
			return runDaemon()
		case "doctor":
			return commands.Doctor()
		case "quality":
			return commands.Quality()
		}
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// parseStopArgs parses the argument list for `ct stop`. Returns the optional
// target agent name, the --clean flag, and any parse error. Order of target
// and --clean is independent. At most one positional argument is accepted.
func parseStopArgs(args []string) (target string, clean bool, err error) {
	for _, a := range args {
		switch {
		case a == "--clean":
			clean = true
		case strings.HasPrefix(a, "--"):
			return "", false, fmt.Errorf("unknown flag: %s", a)
		default:
			if target != "" {
				return "", false, fmt.Errorf("ct stop takes at most one target (got %q and %q)", target, a)
			}
			target = a
		}
	}
	return target, clean, nil
}

func printUsage() {
	fmt.Println(`Usage: ct <command>

Commands:
  init                Set up .company_town/ in project root
  start               Start the Mayor and attach to tmux session
  stop [target] [--clean]
                      Graceful shutdown. Without target, stops every session.
                      With target (daemon, architect, reviewer, artisan-<s>,
                      prole-<name>), stops only that session. --clean removes
                      prole worktrees immediately after signaling (applies only
                      to prole targets; ignored otherwise).
  nuke [target]       Immediate shutdown, no handoffs (target: daemon, architect,
                      mayor, reviewer, prole-<name>, artisan-<specialty>, bare)
  architect           Start the Architect agent
  architect stop      Signal Architect to write handoff and exit
  artisan <specialty> Start an Artisan agent for the given specialty
  artisan <specialty> stop  Signal Artisan to write handoff and exit
  attach <name>       Attach to a running agent session
  dashboard           Open the live agents + tickets TUI
  metrics [--since N] Show system performance metrics (default: last 7 days)
  daemon              Run the daemon (internal — started by ct start)
  doctor              Check system dependencies and project setup
  quality             Live quality metrics TUI dashboard`)
}
