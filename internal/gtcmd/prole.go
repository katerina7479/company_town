package gtcmd

import (
	"fmt"
	"os"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/prole"
	"github.com/katerina7479/company_town/internal/repo"
)

// Prole dispatches gt prole subcommands.
func Prole(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt prole <create|reset|list> ...")
		os.Exit(1)
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn)

	switch args[0] {
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("usage: gt prole create <name>")
		}
		return prole.Create(args[1], cfg, agents)
	case "reset":
		if len(args) < 2 {
			return fmt.Errorf("usage: gt prole reset <name>")
		}
		return prole.Reset(args[1], cfg, agents)
	case "list":
		return proleList(agents, cfg)
	default:
		return fmt.Errorf("unknown prole command: %s", args[0])
	}
}

func proleList(agents *repo.AgentRepo, cfg *config.Config) error {
	proles, err := prole.List(agents, cfg)
	if err != nil {
		return err
	}

	if len(proles) == 0 {
		fmt.Println("No proles.")
		return nil
	}

	for _, p := range proles {
		issue := ""
		if p.CurrentIssue.Valid {
			issue = fmt.Sprintf("  → %s-%d", cfg.TicketPrefix, p.CurrentIssue.Int64)
		}
		sess := ""
		if p.TmuxSession.Valid {
			sess = fmt.Sprintf("  [%s]", p.TmuxSession.String)
		}
		fmt.Printf("  %-15s %-8s%s%s\n", p.Name, p.Status, issue, sess)
	}
	return nil
}
