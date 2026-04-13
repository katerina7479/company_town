package assign

import (
	"fmt"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/prole"
	"github.com/katerina7479/company_town/internal/repo"
)

// ProleCreator is the injection point used in tests. Default is prole.Create.
var ProleCreator = prole.Create

// Execute assigns a ticket to a prole, creating the prole if it does not exist.
// Branch naming: "prole/<name>/<prefix>-<id>" (e.g. "prole/copper/nc-56").
// Agent status and current_issue are intentionally left alone — proles own
// their own status and set it when they pick up work.
func Execute(cfg *config.Config, issues *repo.IssueRepo, agents *repo.AgentRepo, ticketID int, proleName string) error {
	if _, err := agents.Get(proleName); err != nil {
		if err := ProleCreator(proleName, cfg, agents); err != nil {
			return fmt.Errorf("creating prole %s: %w", proleName, err)
		}
	}
	branch := fmt.Sprintf("prole/%s/%s-%d", proleName, cfg.TicketPrefix, ticketID)
	if err := issues.Assign(ticketID, proleName, branch); err != nil {
		return fmt.Errorf("assigning ticket %d: %w", ticketID, err)
	}
	return nil
}
