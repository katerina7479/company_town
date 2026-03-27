package gtcmd

import (
	"fmt"

	"github.com/katerina7479/company_town/internal/db"
)

// Migrate applies any pending database migrations and exits 0.
// Already-applied migrations are skipped (RunMigrations is idempotent).
func Migrate() error {
	conn, _, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := db.RunMigrations(conn); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	fmt.Println("Migrations complete.")
	return nil
}
