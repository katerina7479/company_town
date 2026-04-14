package db

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/katerina7479/company_town/internal/config"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ServerState is runtime-only state for the Dolt server process (not in config).
type ServerState struct {
	PID int `json:"pid"`
}

// InitDolt initializes a Dolt database directory if it doesn't exist.
func InitDolt(doltDir string) error {
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

// StartServer starts a Dolt SQL server using host/port/database from config.
// Writes PID to server.json (runtime state only — connection info is in config.json).
func StartServer(doltDir, ctDir string, cfg *config.DoltConfig) error {
	// Check if already running
	if state, err := loadServerState(ctDir); err == nil {
		if isProcessRunning(state.PID) {
			fmt.Printf("  Dolt server already running (pid=%d, port=%d)\n", state.PID, cfg.Port)
			return nil
		}
		cleanServerState(ctDir)
	}

	if !isPortAvailable(cfg.Host, cfg.Port) {
		return fmt.Errorf("dolt port %d is already in use — either stop the process using that port "+
			"or edit .company_town/config.json dolt.port to a free port", cfg.Port)
	}

	logFile, err := os.OpenFile(filepath.Join(ctDir, "logs", "dolt-server.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening dolt log: %w", err)
	}

	cmd := exec.Command("dolt", "sql-server",
		"--host", cfg.Host,
		"--port", fmt.Sprintf("%d", cfg.Port),
	)
	cmd.Dir = doltDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting dolt server: %w", err)
	}

	if err := saveServerState(ctDir, ServerState{PID: cmd.Process.Pid}); err != nil {
		return fmt.Errorf("saving server state: %w", err)
	}

	if err := waitForServer(cfg.Host, cfg.Port, 10*time.Second); err != nil {
		return fmt.Errorf("dolt server failed to start: %w", err)
	}

	fmt.Printf("  Dolt server started (pid=%d, port=%d)\n", cmd.Process.Pid, cfg.Port)

	cmd.Process.Release()
	logFile.Close()

	return nil
}

// StopServer stops the running Dolt server.
func StopServer(ctDir string) error {
	state, err := loadServerState(ctDir)
	if err != nil {
		return fmt.Errorf("no server state found: %w", err)
	}

	proc, err := os.FindProcess(state.PID)
	if err != nil {
		cleanServerState(ctDir)
		return nil
	}

	if err := proc.Signal(os.Interrupt); err != nil {
		cleanServerState(ctDir)
		return nil
	}

	cleanServerState(ctDir)
	fmt.Printf("  Dolt server stopped (pid=%d)\n", state.PID)
	return nil
}

// Connect returns a database connection using host/port/database from config.
// Creates the database if it doesn't exist.
func Connect(cfg *config.DoltConfig) (*sql.DB, error) {
	// First connect without a database to ensure the DB exists
	rootDSN := fmt.Sprintf("root@tcp(%s:%d)/", cfg.Host, cfg.Port)
	rootConn, err := sql.Open("mysql", rootDSN)
	if err != nil {
		return nil, fmt.Errorf("connecting to dolt: %w", err)
	}

	if err := rootConn.Ping(); err != nil {
		rootConn.Close()
		return nil, fmt.Errorf("dolt server not responding: %w", err)
	}

	// Create database if it doesn't exist
	_, err = rootConn.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", cfg.Database))
	rootConn.Close()
	if err != nil {
		return nil, fmt.Errorf("creating database %s: %w", cfg.Database, err)
	}

	// Now connect to the actual database
	dsn := fmt.Sprintf("root@tcp(%s:%d)/%s?parseTime=true", cfg.Host, cfg.Port, cfg.Database)
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("connecting to dolt database: %w", err)
	}

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("dolt database not responding: %w", err)
	}

	return conn, nil
}

// RunMigrations executes all embedded .sql migration files in order,
// skipping any that have already been recorded in schema_migrations.
func RunMigrations(db *sql.DB) error {
	// Ensure tracking table exists.
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name       VARCHAR(255) NOT NULL PRIMARY KEY,
		applied_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, f).Scan(&count); err != nil {
			return fmt.Errorf("checking migration %s: %w", f, err)
		}
		if count > 0 {
			continue // already applied
		}

		data, err := migrationsFS.ReadFile("migrations/" + f)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", f, err)
		}

		if _, err := db.Exec(string(data)); err != nil {
			return fmt.Errorf("running migration %s: %w", f, err)
		}

		if _, err := db.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, f); err != nil {
			return fmt.Errorf("recording migration %s: %w", f, err)
		}

		fmt.Printf("  applied: %s\n", f)
	}

	return nil
}

func loadServerState(ctDir string) (*ServerState, error) {
	data, err := os.ReadFile(filepath.Join(ctDir, "server.json"))
	if err != nil {
		return nil, err
	}
	var state ServerState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func saveServerState(ctDir string, state ServerState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(ctDir, "server.json"), data, 0644)
}

func cleanServerState(ctDir string) {
	os.Remove(filepath.Join(ctDir, "server.json"))
}

func isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(nil) == nil
}

func isPortAvailable(host string, port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func waitForServer(host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("server not ready after %s", timeout)
}
