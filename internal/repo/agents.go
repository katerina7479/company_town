package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type Agent struct {
	ID               int
	Name             string
	Type             string
	Specialty        sql.NullString
	Status           string
	CurrentIssue     sql.NullInt64
	TmuxSession      sql.NullString
	WorktreePath     sql.NullString
	TimeCreated      time.Time
	TimeEnded        sql.NullTime
	StatusChangedAt  sql.NullTime
}

type AgentRepo struct {
	db *sql.DB
}

func NewAgentRepo(db *sql.DB) *AgentRepo {
	return &AgentRepo{db: db}
}

// Register creates a new agent record.
func (r *AgentRepo) Register(name, agentType string, specialty *string) error {
	var specVal interface{}
	if specialty != nil {
		specVal = *specialty
	}

	_, err := r.db.Exec(
		`INSERT INTO agents (name, type, specialty) VALUES (?, ?, ?)`,
		name, agentType, specVal,
	)
	if err != nil {
		return fmt.Errorf("registering agent %s: %w", name, err)
	}
	return nil
}

// Get retrieves an agent by name.
func (r *AgentRepo) Get(name string) (*Agent, error) {
	row := r.db.QueryRow(
		`SELECT id, name, type, specialty, status, current_issue,
		        tmux_session, worktree_path, time_created, time_ended, status_changed_at
		 FROM agents WHERE name = ?`, name,
	)
	var a Agent
	err := row.Scan(
		&a.ID, &a.Name, &a.Type, &a.Specialty, &a.Status, &a.CurrentIssue,
		&a.TmuxSession, &a.WorktreePath, &a.TimeCreated, &a.TimeEnded, &a.StatusChangedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent %s not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("getting agent: %w", err)
	}
	return &a, nil
}

// UpdateStatus changes an agent's status.
func (r *AgentRepo) UpdateStatus(name, status string) error {
	var timeEnded interface{}
	if status == "dead" {
		timeEnded = time.Now()
	}

	_, err := r.db.Exec(
		`UPDATE agents SET status = ?, time_ended = ?, status_changed_at = ? WHERE name = ?`,
		status, timeEnded, time.Now(), name,
	)
	if err != nil {
		return fmt.Errorf("updating agent status: %w", err)
	}

	// Verify the agent exists (RowsAffected can be 0 if value unchanged)
	var count int
	r.db.QueryRow(`SELECT COUNT(*) FROM agents WHERE name = ?`, name).Scan(&count)
	if count == 0 {
		return fmt.Errorf("agent %s not found", name)
	}
	return nil
}

// SetWorktree sets the worktree path for an agent.
func (r *AgentRepo) SetWorktree(name, path string) error {
	_, err := r.db.Exec(
		`UPDATE agents SET worktree_path = ? WHERE name = ?`,
		path, name,
	)
	return err
}

// SetTmuxSession sets the tmux session name for an agent.
func (r *AgentRepo) SetTmuxSession(name, sessionName string) error {
	_, err := r.db.Exec(
		`UPDATE agents SET tmux_session = ? WHERE name = ?`,
		sessionName, name,
	)
	return err
}

// SetCurrentIssue assigns an issue to an agent.
func (r *AgentRepo) SetCurrentIssue(name string, issueID *int) error {
	var val interface{}
	if issueID != nil {
		val = *issueID
	}

	_, err := r.db.Exec(
		`UPDATE agents SET current_issue = ?, status = 'working', status_changed_at = ? WHERE name = ?`,
		val, time.Now(), name,
	)
	return err
}

// ClearCurrentIssue marks agent as idle with no assigned issue.
func (r *AgentRepo) ClearCurrentIssue(name string) error {
	_, err := r.db.Exec(
		`UPDATE agents SET current_issue = NULL, status = 'idle', status_changed_at = ? WHERE name = ?`,
		time.Now(), name,
	)
	return err
}

// ListByStatus returns agents matching a status.
func (r *AgentRepo) ListByStatus(status string) ([]*Agent, error) {
	rows, err := r.db.Query(
		`SELECT id, name, type, specialty, status, current_issue,
		        tmux_session, worktree_path, time_created, time_ended, status_changed_at
		 FROM agents WHERE status = ? ORDER BY name`, status,
	)
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	defer rows.Close()

	return scanAgentRows(rows)
}

// ListAll returns all agents.
func (r *AgentRepo) ListAll() ([]*Agent, error) {
	rows, err := r.db.Query(
		`SELECT id, name, type, specialty, status, current_issue,
		        tmux_session, worktree_path, time_created, time_ended, status_changed_at
		 FROM agents ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}
	defer rows.Close()

	return scanAgentRows(rows)
}

// FindIdle returns idle agents, optionally filtered by specialty.
func (r *AgentRepo) FindIdle(specialty *string) ([]*Agent, error) {
	var rows *sql.Rows
	var err error

	if specialty != nil {
		rows, err = r.db.Query(
			`SELECT id, name, type, specialty, status, current_issue,
			        tmux_session, worktree_path, time_created, time_ended, status_changed_at
			 FROM agents WHERE status = 'idle' AND specialty = ? ORDER BY name`, *specialty,
		)
	} else {
		rows, err = r.db.Query(
			`SELECT id, name, type, specialty, status, current_issue,
			        tmux_session, worktree_path, time_created, time_ended, status_changed_at
			 FROM agents WHERE status = 'idle' ORDER BY name`,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("finding idle agents: %w", err)
	}
	defer rows.Close()

	return scanAgentRows(rows)
}

// Delete removes an agent row entirely. Used for ephemeral agents (proles)
// whose tmux session no longer exists.
func (r *AgentRepo) Delete(name string) error {
	res, err := r.db.Exec(`DELETE FROM agents WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("deleting agent: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("agent %s not found", name)
	}
	return nil
}

// CountByType returns the number of agents of a given type.
func (r *AgentRepo) CountByType(agentType string) (int, error) {
	var count int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM agents WHERE type = ? AND status != 'dead'`, agentType,
	).Scan(&count)
	return count, err
}

func scanAgentRows(rows *sql.Rows) ([]*Agent, error) {
	var agents []*Agent
	for rows.Next() {
		var a Agent
		err := rows.Scan(
			&a.ID, &a.Name, &a.Type, &a.Specialty, &a.Status, &a.CurrentIssue,
			&a.TmuxSession, &a.WorktreePath, &a.TimeCreated, &a.TimeEnded, &a.StatusChangedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning agent row: %w", err)
		}
		agents = append(agents, &a)
	}
	return agents, rows.Err()
}
