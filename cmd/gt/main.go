package main

import (
	"fmt"
	"os"

	"github.com/katerina7479/company_town/internal/cmdlog"
	"github.com/katerina7479/company_town/internal/gtcmd"
)

// version is set at build time via -ldflags "-X main.version=<tag>".
// Falls back to "dev" for local builds.
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		os.Exit(1)
	}
}

// run executes the gt command given by args (os.Args[1:]). It returns an error
// if no command is given, if the command is unknown, or if the command fails.
// Extracting this from main allows the dispatch logic to be unit-tested without
// calling os.Exit.
func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return fmt.Errorf("no command given")
	}

	cmd := args[0]
	rest := args[1:]

	if cmd == "--version" || cmd == "version" {
		fmt.Println("gt version", version)
		return nil
	}

	// Reject unknown commands before entering log middleware.
	switch cmd {
	case "ticket", "prole", "agent", "pr", "create", "start", "stop", "status", "check", "migrate", "log":
		// valid
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		return fmt.Errorf("unknown command: %s", cmd)
	}

	err := cmdlog.Run(cmdlog.FindLogPath(), "gt", cmdlog.Actor(), args, func() error {
		switch cmd {
		case "ticket":
			return gtcmd.Ticket(rest)
		case "prole":
			return gtcmd.Prole(rest)
		case "agent":
			return gtcmd.Agent(rest)
		case "pr":
			return gtcmd.PR(rest)
		case "start":
			return gtcmd.Start(rest)
		case "stop":
			return gtcmd.Stop(rest)
		case "status":
			return gtcmd.Status()
		case "check":
			return gtcmd.Check(rest)
		case "create":
			return gtcmd.Create(rest)
		case "migrate":
			return gtcmd.Migrate()
		case "log":
			return gtcmd.Log(rest)
		}
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return err
	}

	// Emit a rate-limited stderr warning if the caller's agent row has drifted.
	// Skip on:
	//   - read-only introspection commands (status, check) — drift rendered inline
	//   - agent lifecycle verbs (accept, release, do, status) — actively correcting state
	skipDrift := cmd == "status" || cmd == "check" ||
		(cmd == "agent" && len(rest) > 0 && isAgentExemptVerb(rest[0]))
	gtcmd.WarnDriftOnStdErr(skipDrift)
	return nil
}

// isAgentExemptVerb returns true for gt agent subcommands that should not
// trigger a drift warning. These are verbs that actively fix or report state,
// so a warning would be confusing or redundant.
func isAgentExemptVerb(verb string) bool {
	switch verb {
	case "accept", "release", "do", "status":
		return true
	}
	return false
}

func printUsage() {
	fmt.Println(`Usage: gt <command>

Commands:
  create <reviewer> <name>                                      Create agents
  ticket <create|show|list|ready|assign|status|close|depend>   Manage tickets
  prole <create|reset|list>                                     Manage proles
  agent <register|status>                                        Manage agents
  pr <create>                                                    File PRs
  start <agent>                                                  Start an agent
  stop <agent>                                                   Stop an agent (graceful)
  status                                                         Print system status
  check <run|list|history>                                       Run and view quality checks
  migrate                                                        Apply pending database migrations
  log <tail|show> [flags]                                        Read the command audit log`)
}
