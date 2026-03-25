package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "ticket":
		handleTicket(args)
	case "prole":
		handleProle(args)
	case "agent":
		handleAgent(args)
	case "pr":
		handlePR(args)
	case "status":
		handleStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func handleTicket(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt ticket <create|assign|status|close> ...")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "gt ticket %s: not yet implemented\n", args[0])
	os.Exit(1)
}

func handleProle(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt prole <create|reset> ...")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "gt prole %s: not yet implemented\n", args[0])
	os.Exit(1)
}

func handleAgent(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt agent <register|status> ...")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "gt agent %s: not yet implemented\n", args[0])
	os.Exit(1)
}

func handlePR(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt pr <create> ...")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "gt pr %s: not yet implemented\n", args[0])
	os.Exit(1)
}

func handleStatus() {
	fmt.Fprintf(os.Stderr, "gt status: not yet implemented\n")
	os.Exit(1)
}

func printUsage() {
	fmt.Println(`Usage: gt <command>

Commands:
  ticket <create|assign|status|close>   Manage tickets
  prole <create|reset>                  Manage proles
  agent <register|status>               Manage agents
  pr <create>                           File PRs
  status                                Print system status`)
}
