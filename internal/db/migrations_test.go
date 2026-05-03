package db

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestMigrationsAreSingleStatement enforces the one-statement-per-file
// invariant that RunMigrations relies on. RunMigrations sends each migration
// file to the Dolt driver as a single db.Exec; multi-statement files fail
// with a syntax error and the system ends up half-migrated. nc-282 was the
// incident that prompted this guard.
func TestMigrationsAreSingleStatement(t *testing.T) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("reading embedded migrations: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatalf("reading %s: %v", e.Name(), err)
		}
		stripped := stripSQLComments(string(data))
		// Count terminators that aren't inside a string literal. We don't
		// support quoted ';' in migrations today, so a literal count is
		// sufficient and keeps this test simple.
		count := strings.Count(stripped, ";")
		if count != 1 {
			t.Errorf("%s: expected exactly 1 statement-terminating ';' (got %d). "+
				"RunMigrations executes each file as a single Exec; split multi-statement "+
				"migrations into separate numbered files.", e.Name(), count)
		}
	}
}

// stripSQLComments removes line comments (-- ...) and blank lines so the
// terminator count reflects only executable SQL.
func stripSQLComments(s string) string {
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		// Drop trailing inline comment if present.
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

// TestRunMigrations_idempotent verifies that calling RunMigrations on an
// already-fully-migrated database returns nil and adds no new rows. This is
// the safety guarantee that makes it safe to call RunMigrations on every
// ct start / gt start invocation — a fully up-to-date install is a no-op.
func TestRunMigrations_idempotent(t *testing.T) {
	conn, err := NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("reading embedded migrations: %v", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}

	// Pre-populate all migrations as already applied, simulating a fully up-to-date install.
	for _, name := range names {
		if _, err := conn.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			t.Fatalf("inserting %s into schema_migrations: %v", name, err)
		}
	}

	if err := RunMigrations(conn); err != nil {
		t.Fatalf("RunMigrations on fully-migrated DB: %v", err)
	}

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("counting schema_migrations: %v", err)
	}
	if count != len(names) {
		t.Errorf("schema_migrations row count = %d, want %d (idempotent — no new rows)", count, len(names))
	}
}

// TestRunMigrations_upgradePathAppliesNewMigrations simulates the binary-upgrade
// scenario described in nc-284: an existing project has an older schema version
// (migrations 001-009 applied) and a new binary ships migrations 010 and 011.
// RunMigrations must apply the new migrations and record them in schema_migrations.
//
// The test uses a minimal SQLite DB with only the columns needed by migrations
// 010 (ALTER TABLE ADD COLUMN) and 011 (UPDATE), confirming they succeed on the
// SQLite test driver. Earlier migrations are pre-marked as applied and skipped.
func TestRunMigrations_upgradePathAppliesNewMigrations(t *testing.T) {
	conn, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer conn.Close()

	// Minimal issues table without ci_running_entered_at — simulates schema at v009.
	_, err = conn.Exec(`CREATE TABLE issues (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		status     TEXT    NOT NULL DEFAULT 'draft',
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("creating issues table: %v", err)
	}

	_, err = conn.Exec(`CREATE TABLE schema_migrations (
		name       TEXT     NOT NULL PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("creating schema_migrations table: %v", err)
	}

	// Mark 001-009 as already applied (the "old binary" state).
	preMigrations := []string{
		"001_create_issues.sql",
		"002_create_agents.sql",
		"003_create_issue_dependencies.sql",
		"004_add_agent_status_changed_at.sql",
		"005_create_quality_metrics.sql",
		"006_add_priority_to_issues.sql",
		"007_retired_priority_remap.sql",
		"008_add_repair_cycle_count.sql",
		"009_add_repair_reason.sql",
	}
	for _, m := range preMigrations {
		if _, err := conn.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, m); err != nil {
			t.Fatalf("pre-inserting %s: %v", m, err)
		}
	}

	// RunMigrations should now apply 010 and 011.
	if err := RunMigrations(conn); err != nil {
		t.Fatalf("RunMigrations upgrade path: %v", err)
	}

	// Both new migrations must appear in schema_migrations.
	for _, m := range []string{"010_ci_running_entered_at.sql", "011_backfill_ci_running_entered_at.sql"} {
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, m).Scan(&count); err != nil {
			t.Fatalf("querying schema_migrations for %s: %v", m, err)
		}
		if count != 1 {
			t.Errorf("migration %s not found in schema_migrations after upgrade", m)
		}
	}

	// Verify the column was actually added by migration 010.
	rows, err := conn.Query(`SELECT ci_running_entered_at FROM issues LIMIT 1`)
	if err != nil {
		t.Fatalf("querying ci_running_entered_at column: %v", err)
	}
	defer rows.Close() //nolint:staticcheck // rows is closed below; defer is belt-and-suspenders
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error after querying ci_running_entered_at: %v", err)
	}
}
