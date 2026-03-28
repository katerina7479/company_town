package main

import (
	"fmt"
	"os"

	"github.com/katerina7479/company_town/internal/commands"
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
	case "init":
		force := false
		for _, a := range args {
			if a == "--force" {
				force = true
			}
		}
		err = commands.Init(force)
	case "start":
		err = commands.Start()
	case "stop":
		err = commands.Stop()
	case "nuke":
		err = commands.Nuke()
	case "architect":
		if len(args) > 0 && args[0] == "stop" {
			err = commands.ArchitectStop()
		} else {
			err = commands.Architect()
		}
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
			err = commands.ArtisanStop(specialty)
		} else {
			err = commands.Artisan(specialty)
		}
	case "janitor":
		if len(args) > 0 && args[0] == "stop" {
			err = commands.JanitorStop()
		} else {
			err = commands.Janitor()
		}
	case "attach":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: ct attach <session-name>")
			os.Exit(1)
		}
		err = commands.Attach(args[0])
	case "dashboard":
		err = commands.Dashboard()
	case "daemon":
		err = runDaemon()
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
	fmt.Println(`Usage: ct <command>

Commands:
  init [--force]      Set up .company_town/ in project root
  start               Start the Mayor and attach to tmux session
  stop                Graceful shutdown with handoffs
  nuke                Immediate shutdown, no handoffs
  architect           Start the Architect agent
  architect stop      Signal Architect to write handoff and exit
  artisan <specialty> Start an Artisan agent for the given specialty
  artisan <specialty> stop  Signal Artisan to write handoff and exit
  janitor             Start the Janitor agent
  janitor stop        Signal Janitor to write handoff and exit
  attach <name>       Attach to a running agent session
  dashboard           Open the live agents + tickets TUI
  daemon              Run the daemon (internal — started by ct start)`)
}
