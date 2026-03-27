package health

import (
	"database/sql"
	"fmt"
	"os/exec"
)

// DBCheck verifies that the database is reachable.
type DBCheck struct {
	db *sql.DB
}

// NewDBCheck creates a DBCheck for the given connection.
func NewDBCheck(db *sql.DB) *DBCheck {
	return &DBCheck{db: db}
}

func (c *DBCheck) Name() string { return "database" }

func (c *DBCheck) Run() Result {
	if err := c.db.Ping(); err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: fmt.Sprintf("database unreachable: %v", err),
		}
	}
	return Result{Name: c.Name(), Status: StatusOK, Message: "connected"}
}

// SessionCheck verifies that a named tmux session is running.
type SessionCheck struct {
	sessionName   string
	sessionExists func(string) bool
}

// NewSessionCheck creates a check for the given session name.
// sessionExists should be session.Exists in production; injectable for tests.
func NewSessionCheck(sessionName string, sessionExists func(string) bool) *SessionCheck {
	return &SessionCheck{sessionName: sessionName, sessionExists: sessionExists}
}

func (c *SessionCheck) Name() string {
	return fmt.Sprintf("session:%s", c.sessionName)
}

func (c *SessionCheck) Run() Result {
	if !c.sessionExists(c.sessionName) {
		return Result{
			Name:    c.Name(),
			Status:  StatusWarn,
			Message: fmt.Sprintf("session %q is not running", c.sessionName),
		}
	}
	return Result{Name: c.Name(), Status: StatusOK, Message: "running"}
}

// BinaryCheck verifies that a required binary is on PATH.
type BinaryCheck struct {
	binary string
}

// NewBinaryCheck creates a check that confirms the named binary is available.
func NewBinaryCheck(binary string) *BinaryCheck {
	return &BinaryCheck{binary: binary}
}

func (c *BinaryCheck) Name() string { return fmt.Sprintf("binary:%s", c.binary) }

func (c *BinaryCheck) Run() Result {
	if _, err := exec.LookPath(c.binary); err != nil {
		return Result{
			Name:    c.Name(),
			Status:  StatusFail,
			Message: fmt.Sprintf("%q not found on PATH", c.binary),
		}
	}
	return Result{Name: c.Name(), Status: StatusOK, Message: "found"}
}
