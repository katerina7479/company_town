package repo

import "fmt"

// DriftEntry describes one detected inconsistency between an agent row and
// the tickets table.
type DriftEntry struct {
	AgentName string
	Reason    string
}

// CheckDrift cross-references every agent row against the tickets table and
// returns a (possibly empty) list of inconsistencies. The three categories
// flagged are:
//
//   - Idle agent whose current_issue pointer is still set.
//   - Working agent whose current_issue ticket is closed.
//   - Agent pointing at a ticket assigned to a different agent.
//
// prefix is used to format ticket IDs in reason strings (e.g. "nc").
// A non-nil error means the DB query failed; a nil error with a non-empty
// slice means drift was found.
func CheckDrift(agents *AgentRepo, issues *IssueRepo, prefix string) ([]DriftEntry, error) {
	all, err := agents.ListAll()
	if err != nil {
		return nil, fmt.Errorf("drift: listing agents: %w", err)
	}

	var entries []DriftEntry

	for _, a := range all {
		if !a.CurrentIssue.Valid {
			continue // no issue pointer — nothing to check
		}
		id := int(a.CurrentIssue.Int64)
		ticketRef := fmt.Sprintf("%s-%d", prefix, id)

		// Category (a): idle agent with non-null issue pointer.
		if a.Status == StatusIdle {
			entries = append(entries, DriftEntry{
				AgentName: a.Name,
				Reason:    fmt.Sprintf("%s is idle but still references %s — run 'gt agent release'", a.Name, ticketRef),
			})
			continue // no need to check ticket details for idle agents
		}

		// For working/dead agents, look up the ticket.
		issue, err := issues.Get(id)
		if err != nil {
			// Ticket missing entirely — orphaned pointer.
			entries = append(entries, DriftEntry{
				AgentName: a.Name,
				Reason:    fmt.Sprintf("%s points at %s which no longer exists — run 'gt agent release'", a.Name, ticketRef),
			})
			continue
		}

		// Category (b): working agent whose ticket is closed.
		if issue.Status == StatusClosed {
			entries = append(entries, DriftEntry{
				AgentName: a.Name,
				Reason:    fmt.Sprintf("%s is working on %s but ticket is closed — run 'gt agent release'", a.Name, ticketRef),
			})
		}

		// Category (c): agent pointing at ticket assigned to someone else.
		if issue.Assignee.Valid && issue.Assignee.String != "" && issue.Assignee.String != a.Name {
			entries = append(entries, DriftEntry{
				AgentName: a.Name,
				Reason: fmt.Sprintf("%s points at %s but ticket is assigned to %s — run 'gt agent release'",
					a.Name, ticketRef, issue.Assignee.String),
			})
		}
	}

	return entries, nil
}
