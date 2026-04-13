package daemon

import (
	"github.com/katerina7479/company_town/internal/assign"
)

// handleAssignments pairs unassigned selectable tickets with available prole
// slots and calls assign.Execute on each pair. It runs on every daemon tick.
//
// Slot sources (in order):
//  1. Idle proles already registered in the DB.
//  2. Net-new prole names (from the canonical metal list) up to the max_proles
//     cap. assign.Execute creates the prole if it does not yet exist.
//
// Ordering is FIFO — candidates are returned in priority order by
// issues.Selectable() and paired left-to-right with slots.
func (d *Daemon) handleAssignments() {
	candidates, err := d.issues.Selectable()
	if err != nil {
		d.logger.Printf("error listing selectable tickets: %v", err)
		return
	}
	if len(candidates) == 0 {
		return
	}

	busy, err := d.issues.BusyAssignees()
	if err != nil {
		d.logger.Printf("error listing busy assignees: %v", err)
		return
	}

	idleAgents, err := d.agents.FindIdle(nil)
	if err != nil {
		d.logger.Printf("error listing idle agents: %v", err)
		return
	}
	var slots []string
	for _, a := range idleAgents {
		if a.Type != "prole" {
			continue
		}
		if busy[a.Name] {
			continue
		}
		slots = append(slots, a.Name)
	}

	existing, err := d.agents.CountByType("prole")
	if err != nil {
		d.logger.Printf("error counting proles: %v", err)
		return
	}
	if d.cfg.MaxProles > 0 && existing < d.cfg.MaxProles {
		headroom := d.cfg.MaxProles - existing
		for i := 0; i < headroom; i++ {
			name, err := d.agents.FirstAvailableMetalName()
			if err != nil {
				d.logger.Printf("error finding available prole name: %v", err)
				break
			}
			if name == "" {
				break // all metal names taken
			}
			if busy[name] {
				continue
			}
			slots = append(slots, name)
		}
	}

	if len(slots) == 0 {
		return
	}

	n := len(candidates)
	if len(slots) < n {
		n = len(slots)
	}

	assigned := 0
	for i := 0; i < n; i++ {
		t := candidates[i]
		p := slots[i]
		if err := assign.Execute(d.cfg, d.issues, d.agents, t.ID, p); err != nil {
			d.logger.Printf("error assigning ticket %d to %s: %v", t.ID, p, err)
			continue
		}
		d.logger.Printf("assigned ticket %d to %s", t.ID, p)
		assigned++
	}
	d.logger.Printf("%d candidate(s), %d slot(s), %d assigned", len(candidates), len(slots), assigned)
}
