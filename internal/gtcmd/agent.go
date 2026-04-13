package gtcmd

import (
	"fmt"
	"os"

	"github.com/katerina7479/company_town/internal/cmdlog"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// Agent dispatches gt agent subcommands.
func Agent(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt agent <register|status> ...")
		os.Exit(1)
	}

	conn, _, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn, nil)

	switch args[0] {
	case "register":
		return agentRegister(agents, args[1:])
	case "status":
		return agentStatus(agents, args[1:])
	default:
		return fmt.Errorf("unknown agent command: %s", args[0])
	}
}

func agentRegister(agents *repo.AgentRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt agent register <name> <type> [--specialty <s>]")
	}

	name := args[0]
	agentType := args[1]
	var specialty *string

	for i := 2; i < len(args); i++ {
		if args[i] == "--specialty" && i+1 < len(args) {
			i++
			s := args[i]
			specialty = &s
		}
	}

	if err := agents.Register(name, agentType, specialty); err != nil {
		return err
	}

	fmt.Printf("Registered agent %s (type=%s)\n", name, agentType)
	return nil
}

func agentStatus(agents *repo.AgentRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt agent status <name> <idle|working|dead> [--issue <id>]")
	}

	name := args[0]
	status := args[1]
	var issueID *int

	for i := 2; i < len(args); i++ {
		if args[i] == "--issue" && i+1 < len(args) {
			i++
			v, err := parseTicketID(args[i])
			if err != nil {
				return fmt.Errorf("invalid issue ID: %s", args[i])
			}
			issueID = &v
		}
	}

	// Capture before status for annotation; tolerate lookup failure.
	var before string
	if a, err := agents.Get(name); err == nil {
		before = a.Status
	}

	switch {
	case issueID != nil:
		if status != "working" {
			return fmt.Errorf("--issue requires status \"working\", got %q", status)
		}
		if err := agents.SetCurrentIssue(name, issueID); err != nil {
			return err
		}
		cmdlog.Annotate("agent="+name, before, "working")
		fmt.Printf("Agent %s → working (issue %d)\n", name, *issueID)
	case status == "idle":
		if err := agents.ClearCurrentIssue(name); err != nil {
			return err
		}
		cmdlog.Annotate("agent="+name, before, "idle")
		fmt.Printf("Agent %s → idle\n", name)
	default:
		if err := agents.UpdateStatus(name, status); err != nil {
			return err
		}
		cmdlog.Annotate("agent="+name, before, status)
		fmt.Printf("Agent %s → %s\n", name, status)
	}

	return nil
}
