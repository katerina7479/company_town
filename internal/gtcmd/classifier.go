// Package gtcmd implements the gt agent CLI subcommands.
package gtcmd

import (
	"fmt"

	"github.com/katerina7479/company_town/internal/config"
)

// agentSpec describes the lifecycle properties of a named agent role.
// artisan-* and daemon are handled separately due to their dynamic or
// entirely distinct nature and are not represented here.
type agentSpec struct {
	// agentType is the role string stored in the agents table.
	agentType string
	// templateType is the key passed to commands.WriteClaudeMD.
	templateType string
	// agentSubDir is the directory name under ctDir/agents/ for this agent.
	// Empty means the agent is not startable via gt start (e.g. mayor).
	agentSubDir string
	// hasSignalFile indicates whether a handoff_requested signal file should
	// be written when the agent is stopped.
	hasSignalFile bool
	// configFor returns the AgentConfig for this role from the project config.
	configFor func(cfg *config.Config) config.AgentConfig
	// promptFn builds the startup prompt text for this agent. Nil for agents
	// that are not started via gt start (e.g. mayor).
	promptFn func(cfg *config.Config) string
}

// agentRegistry maps canonical agent names to their specs. artisan-* and daemon
// are excluded because they require dynamic specialty-based or entirely different
// lifecycle logic that cannot be expressed as a static registry entry.
var agentRegistry = map[string]agentSpec{
	"architect": {
		agentType:     "architect",
		templateType:  "architect",
		agentSubDir:   "architect",
		hasSignalFile: true,
		configFor:     func(cfg *config.Config) config.AgentConfig { return cfg.Agents.Architect },
		promptFn: func(cfg *config.Config) string {
			return fmt.Sprintf(
				"You are the Architect. Ticket prefix: %s. "+
					"Read your CLAUDE.md for instructions. "+
					"Check memory/handoff.md to resume previous work. "+
					"Begin your patrol loop: check for draft tickets and spec them out.",
				cfg.TicketPrefix,
			)
		},
	},
	"reviewer": {
		agentType:     "reviewer",
		templateType:  "reviewer",
		agentSubDir:   "reviewer",
		hasSignalFile: false,
		configFor:     func(cfg *config.Config) config.AgentConfig { return cfg.Agents.Reviewer },
		promptFn: func(cfg *config.Config) string {
			return fmt.Sprintf(
				"You are the Reviewer. Ticket prefix: %s. "+
					"Read your CLAUDE.md for instructions. "+
					"Check memory/handoff.md to resume previous work. "+
					"Begin patrol: check for in_review tickets and review their PRs.",
				cfg.TicketPrefix,
			)
		},
	},
	// mayor is present for config lookup only — it is not started via gt start.
	"mayor": {
		configFor: func(cfg *config.Config) config.AgentConfig { return cfg.Agents.Mayor },
	},
}
