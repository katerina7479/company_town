package main

import (
	"fmt"
	"os"

	"github.com/katerina7479/company_town/internal/gtcmd"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "ticket":
		err = gtcmd.Ticket(args)
	case "prole":
		err = gtcmd.Prole(args)
	case "agent":
		err = gtcmd.Agent(args)
	case "pr":
		err = gtcmd.PR(args)
	case "start":
		err = gtcmd.Start(args)
	case "stop":
		err = gtcmd.Stop(args)
	case "status":
		err = gtcmd.Status()
	case "check":
		err = gtcmd.Check(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Usage: gt <command>

Commands:
  ticket <create|show|list|ready|assign|status|close|depend>   Manage tickets
  prole <create|reset|list>                                     Manage proles
  agent <register|status>                                        Manage agents
  pr <create>                                                    File PRs
  start <agent>                                                  Start an agent
  stop <agent>                                                   Stop an agent (graceful)
  status                                                         Print system status
  check <run|list|history>                                       Run and view quality checks`)
}
