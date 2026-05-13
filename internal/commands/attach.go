package commands

import (
	"fmt"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/session"
)

// Attach implements `ct attach <name>` — attach to an existing agent session.
func Attach(name string) error {
	if projectRoot, findErr := db.FindProjectRoot(); findErr == nil {
		if cfg, cfgErr := config.Load(projectRoot); cfgErr == nil {
			applySessionPrefix(cfg)
		}
	}

	sessionName := session.SessionName(name)

	if !session.Exists(sessionName) {
		return fmt.Errorf("session %q is not running", name)
	}

	return session.Attach(sessionName)
}
