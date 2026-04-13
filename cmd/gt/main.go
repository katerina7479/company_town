package main

import (
	"fmt"
	"os"

	"github.com/katerina7479/company_town/internal/cmdlog"
	"github.com/katerina7479/company_town/internal/gtcmd"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	// Reject unknown commands before entering log middleware.
	switch cmd {
	case "ticket", "prole", "agent", "pr", "create", "start", "stop", "status", "check", "migrate":
		// valid
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}

	err := cmdlog.Run(cmdlog.FindLogPath(), "gt", cmdlog.Actor(), os.Args[1:], func() error {
		switch cmd {
		case "ticket":
			return gtcmd.Ticket(args)
		case "prole":
			return gtcmd.Prole(args)
		case "agent":
			return gtcmd.Agent(args)
		case "pr":
			return gtcmd.PR(args)
		case "start":
			return gtcmd.Start(args)
		case "stop":
			return gtcmd.Stop(args)
		case "status":
			return gtcmd.Status()
		case "check":
			return gtcmd.Check(args)
		case "create":
			return gtcmd.Create(args)
		case "migrate":
			return gtcmd.Migrate()
		}
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
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
  migrate                                                        Apply pending database migrations`)
}
