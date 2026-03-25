package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/katerina7479/company_town/internal/config"
)

// OpenFromWorkingDir finds the project root (by looking for .company_town/),
// loads config, and returns a DB connection.
func OpenFromWorkingDir() (*sql.DB, *config.Config, error) {
	projectRoot, err := FindProjectRoot()
	if err != nil {
		return nil, nil, err
	}

	cfg, err := config.Load(projectRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	conn, err := Connect(&cfg.Dolt)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to database: %w", err)
	}

	return conn, cfg, nil
}

// FindProjectRoot walks up from the current directory looking for .company_town/.
func FindProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, config.DirName)); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not in a company town project (no %s/ found)", config.DirName)
		}
		dir = parent
	}
}
