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
	if d.obs != nil {
		d.obs.assignCandidates = len(candidates)
	}
	if len(candidates) == 0 {
		return
	}

	d.logger.Printf("handleAssignments: %d candidate(s) ready for assignment", len(candidates))

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

	d.logger.Printf("handleAssignments: %d idle prole slot(s) available", len(slots))

	existing, err := d.agents.CountByType("prole")
	if err != nil {
		d.logger.Printf("error counting proles: %v", err)
		return
	}

	d.logger.Printf("handleAssignments: %d existing prole(s), cap=%d", existing, d.cfg.MaxProles)

	headroomAdded := 0
	if d.cfg.MaxProles > 0 && existing < d.cfg.MaxProles {
		headroom := d.cfg.MaxProles - existing
		pendingNames := make(map[string]bool)
		for i := 0; i < headroom; i++ {
			name, err := d.agents.FirstAvailableMetalNameExcluding(pendingNames)
			if err != nil {
				d.logger.Printf("error finding available prole name: %v", err)
				break
			}
			if name == "" {
				d.logger.Printf("handleAssignments: metal name list exhausted — cannot expand further")
				break
			}
			if busy[name] {
				d.logger.Printf("handleAssignments: metal name %q already busy, skipping headroom slot", name)
				pendingNames[name] = true
				continue
			}
			d.logger.Printf("handleAssignments: adding headroom slot %q (will be created on assign)", name)
			slots = append(slots, name)
			pendingNames[name] = true
			headroomAdded++
		}
	}

	if d.obs != nil {
		d.obs.assignSlots = len(slots)
	}

	if len(slots) == 0 {
		if d.cfg.MaxProles > 0 && existing >= d.cfg.MaxProles {
			d.logger.Printf("handleAssignments: prole cap reached (%d/%d), all proles appear busy — %d ticket(s) blocked; check for stuck proles with 'gt status'",
				existing, d.cfg.MaxProles, len(candidates))
		} else {
			d.logger.Printf("handleAssignments: no slots available for %d candidate(s) (cap disabled, no idle proles)", len(candidates))
		}
		return
	}

	d.logger.Printf("handleAssignments: %d total slot(s) (%d idle, %d new headroom)",
		len(slots), len(slots)-headroomAdded, headroomAdded)

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
	if d.obs != nil {
		d.obs.assignPaired = assigned
	}
	d.logger.Printf("handleAssignments: assigned %d/%d candidate(s)", assigned, len(candidates))
}
