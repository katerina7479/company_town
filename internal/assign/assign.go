package assign

import (
	"fmt"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/prole"
	"github.com/katerina7479/company_town/internal/repo"
)

// ProleCreator is the injection point used in tests. Default is prole.Create.
var ProleCreator = prole.Create

// WorktreeSwitcher is called after assigning a repair ticket to switch the
// prole's worktree to the existing branch. Override in tests to avoid real git.
var WorktreeSwitcher = prole.SwitchToBranch

// Execute assigns a ticket to a prole, creating the prole if it does not exist.
// Branch naming: "prole/<name>/<prefix>-<id>" (e.g. "prole/copper/nc-56") on
// first assignment. On re-assignment the existing branch is preserved so the
// new prole continues work on the same branch and any open PR tracks incoming
// commits correctly.
// When the ticket has a pre-existing branch (repair scenario), the prole's
// worktree is switched to that branch via WorktreeSwitcher so the prole can
// push to the same PR without manual branch setup.
// Agent status and current_issue are intentionally left alone — proles own
// their own status and set it when they pick up work.
func Execute(cfg *config.Config, issues *repo.IssueRepo, agents *repo.AgentRepo, ticketID int, proleName string) error {
	if _, err := agents.Get(proleName); err != nil {
		if err := ProleCreator(proleName, cfg, agents); err != nil {
			return fmt.Errorf("creating prole %s: %w", proleName, err)
		}
	}
	ticket, err := issues.Get(ticketID)
	if err != nil {
		return fmt.Errorf("getting ticket %d: %w", ticketID, err)
	}
	freshBranch := config.ProleBranchName(cfg.TicketPrefix, proleName, ticketID)
	hasExistingBranch := ticket.Branch.Valid && ticket.Branch.String != ""
	branch := freshBranch
	if hasExistingBranch {
		branch = ticket.Branch.String
	}
	if err := issues.Assign(ticketID, proleName, branch); err != nil {
		return fmt.Errorf("assigning ticket %d: %w", ticketID, err)
	}

	// If the ticket has a pre-existing branch (repair or cross-prole reassignment),
	// switch the prole's worktree to that branch so the prole starts on the right
	// commits and pushes to the same PR. Non-fatal: prole CLAUDE.md covers manual
	// recovery via git checkout.
	if hasExistingBranch {
		agent, err := agents.Get(proleName)
		if err == nil && agent.WorktreePath.Valid && agent.WorktreePath.String != "" {
			barePath := prole.BareRepoPath(cfg)
			if switchErr := WorktreeSwitcher(agent.WorktreePath.String, barePath, branch); switchErr != nil {
				// Non-fatal: log but do not block the assignment.
				_ = switchErr
			}
		}
	}

	return nil
}
