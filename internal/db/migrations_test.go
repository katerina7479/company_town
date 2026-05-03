package db

import (
	"strings"
	"testing"
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
