package db

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

// InitDolt initializes a Dolt database directory if it doesn't exist.
func InitDolt(doltDir string, dbName string) error {
	if _, err := os.Stat(filepath.Join(doltDir, ".dolt")); err == nil {
		return nil // already initialized
	}

	if err := os.MkdirAll(doltDir, 0755); err != nil {
		return fmt.Errorf("creating dolt dir: %w", err)
	}

	cmd := exec.Command("dolt", "init", "--name", "company-town", "--email", "ct@localhost")
	cmd.Dir = doltDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("dolt init: %w", err)
	}

	return nil
}

// StartServer starts a Dolt SQL server in the background.
// Returns the process so the caller can stop it later.
func StartServer(doltDir string, port int) (*exec.Cmd, error) {
	cmd := exec.Command("dolt", "sql-server",
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", port),
		"--no-auto-commit",
	)
	cmd.Dir = doltDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting dolt server: %w", err)
	}

	return cmd, nil
}

// Connect returns a database connection to the Dolt SQL server.
func Connect(dbName string, port int) (*sql.DB, error) {
	dsn := fmt.Sprintf("root@tcp(127.0.0.1:%d)/%s", port, dbName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to dolt: %w", err)
	}
	return db, nil
}

// RunMigrations executes all .sql files in the migrations directory in order.
func RunMigrations(db *sql.DB, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		path := filepath.Join(migrationsDir, f)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", f, err)
		}

		if _, err := db.Exec(string(data)); err != nil {
			return fmt.Errorf("running migration %s: %w", f, err)
		}

		fmt.Printf("  applied: %s\n", f)
	}

	return nil
}
