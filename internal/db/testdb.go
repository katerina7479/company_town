package db

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// SQLite-compatible schema for testing
const testSchema = `
CREATE TABLE IF NOT EXISTS issues (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  issue_type    TEXT NOT NULL DEFAULT 'task',
  status        TEXT NOT NULL DEFAULT 'draft',
  title         TEXT NOT NULL,
  description   TEXT,
  specialty     TEXT,
  branch        TEXT,
  pr_number     INTEGER,
  assignee      TEXT,
  parent_id     INTEGER,
  priority      TEXT,
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  closed_at     DATETIME,
  FOREIGN KEY (parent_id) REFERENCES issues(id)
);

CREATE TABLE IF NOT EXISTS agents (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  name          TEXT NOT NULL UNIQUE,
  type          TEXT NOT NULL,
  specialty     TEXT,
  status        TEXT NOT NULL DEFAULT 'idle',
  current_issue INTEGER,
  tmux_session  TEXT,
  worktree_path TEXT,
  time_created       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  time_ended         DATETIME,
  status_changed_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (current_issue) REFERENCES issues(id)
);

CREATE TABLE IF NOT EXISTS issue_dependencies (
  issue_id      INTEGER NOT NULL,
  depends_on_id INTEGER NOT NULL,
  PRIMARY KEY (issue_id, depends_on_id),
  FOREIGN KEY (issue_id) REFERENCES issues(id),
  FOREIGN KEY (depends_on_id) REFERENCES issues(id)
);

CREATE TABLE IF NOT EXISTS quality_metrics (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  check_name TEXT     NOT NULL,
  status     TEXT     NOT NULL,
  output     TEXT,
  value      REAL,
  run_at     DATETIME NOT NULL,
  error      TEXT
);

CREATE TABLE IF NOT EXISTS schema_migrations (
  name       TEXT     NOT NULL PRIMARY KEY,
  applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// NewTestDB creates an in-memory SQLite database for testing.
func NewTestDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(testSchema); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
