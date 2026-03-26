package repo

import (
	"database/sql"
	"fmt"
	"time"
)

type Issue struct {
	ID          int
	IssueType   string
	Status      string
	Title       string
	Description sql.NullString
	Specialty   sql.NullString
	Branch      sql.NullString
	PRNumber    sql.NullInt64
	Assignee    sql.NullString
	ParentID    sql.NullInt64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ClosedAt    sql.NullTime
}

// Valid issue statuses.
var ValidStatuses = []string{
	"draft", "open", "in_progress", "in_review",
	"reviewed", "repairing", "closed",
}

// Valid issue types.
var ValidTypes = []string{"task", "epic", "bug", "refactor"}

type IssueRepo struct {
	db *sql.DB
}

func NewIssueRepo(db *sql.DB) *IssueRepo {
	return &IssueRepo{db: db}
}

// Create inserts a new issue and returns its ID.
func (r *IssueRepo) Create(title, issueType string, parentID *int, specialty *string) (int, error) {
	if issueType == "" {
		issueType = "task"
	}

	var parentVal interface{}
	if parentID != nil {
		parentVal = *parentID
	}
	var specVal interface{}
	if specialty != nil {
		specVal = *specialty
	}

	result, err := r.db.Exec(
		`INSERT INTO issues (title, issue_type, parent_id, specialty) VALUES (?, ?, ?, ?)`,
		title, issueType, parentVal, specVal,
	)
	if err != nil {
		return 0, fmt.Errorf("creating issue: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting issue id: %w", err)
	}

	return int(id), nil
}

// Get retrieves a single issue by ID.
func (r *IssueRepo) Get(id int) (*Issue, error) {
	row := r.db.QueryRow(
		`SELECT id, issue_type, status, title, description, specialty, branch,
		        pr_number, assignee, parent_id, created_at, updated_at, closed_at
		 FROM issues WHERE id = ?`, id,
	)
	return scanIssue(row)
}

// List returns issues filtered by status (optional).
func (r *IssueRepo) List(status string) ([]*Issue, error) {
	var rows *sql.Rows
	var err error

	if status != "" {
		rows, err = r.db.Query(
			`SELECT id, issue_type, status, title, description, specialty, branch,
			        pr_number, assignee, parent_id, created_at, updated_at, closed_at
			 FROM issues WHERE status = ? ORDER BY id`, status,
		)
	} else {
		rows, err = r.db.Query(
			`SELECT id, issue_type, status, title, description, specialty, branch,
			        pr_number, assignee, parent_id, created_at, updated_at, closed_at
			 FROM issues ORDER BY id`,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}
	defer rows.Close()

	var issues []*Issue
	for rows.Next() {
		issue, err := scanIssueRow(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// UpdateStatus changes an issue's status.
func (r *IssueRepo) UpdateStatus(id int, status string) error {
	var closedAt interface{}
	if status == "closed" {
		closedAt = time.Now()
	}

	result, err := r.db.Exec(
		`UPDATE issues SET status = ?, closed_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status, closedAt, id,
	)
	if err != nil {
		return fmt.Errorf("updating issue status: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue %d not found", id)
	}
	return nil
}

// Assign sets the assignee and branch on an issue.
func (r *IssueRepo) Assign(id int, assignee, branch string) error {
	result, err := r.db.Exec(
		`UPDATE issues SET assignee = ?, branch = ?, status = 'in_progress',
		        updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		assignee, branch, id,
	)
	if err != nil {
		return fmt.Errorf("assigning issue: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue %d not found", id)
	}
	return nil
}

// Delete removes an issue by ID.
func (r *IssueRepo) Delete(id int) error {
	// Remove dependencies first
	r.db.Exec(`DELETE FROM issue_dependencies WHERE issue_id = ? OR depends_on_id = ?`, id, id)

	result, err := r.db.Exec(`DELETE FROM issues WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting issue: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue %d not found", id)
	}
	return nil
}

// SetPR sets the PR number on an issue.
func (r *IssueRepo) SetPR(id, prNumber int) error {
	_, err := r.db.Exec(
		`UPDATE issues SET pr_number = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		prNumber, id,
	)
	return err
}

// ListWithPRs returns non-closed issues that have a PR number set.
func (r *IssueRepo) ListWithPRs() ([]*Issue, error) {
	rows, err := r.db.Query(
		`SELECT id, issue_type, status, title, description, specialty, branch,
		        pr_number, assignee, parent_id, created_at, updated_at, closed_at
		 FROM issues WHERE pr_number IS NOT NULL AND status != 'closed'
		 ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing issues with PRs: %w", err)
	}
	defer rows.Close()

	var issues []*Issue
	for rows.Next() {
		issue, err := scanIssueRow(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

// Close closes an issue.
func (r *IssueRepo) Close(id int) error {
	return r.UpdateStatus(id, "closed")
}

// AddDependency records that issueID depends on dependsOnID.
func (r *IssueRepo) AddDependency(issueID, dependsOnID int) error {
	_, err := r.db.Exec(
		`INSERT INTO issue_dependencies (issue_id, depends_on_id) VALUES (?, ?)`,
		issueID, dependsOnID,
	)
	if err != nil {
		return fmt.Errorf("adding dependency: %w", err)
	}
	return nil
}

// GetDependencies returns the IDs of issues that issueID depends on.
func (r *IssueRepo) GetDependencies(issueID int) ([]int, error) {
	rows, err := r.db.Query(
		`SELECT depends_on_id FROM issue_dependencies WHERE issue_id = ?`,
		issueID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting dependencies: %w", err)
	}
	defer rows.Close()

	var deps []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning dependency: %w", err)
		}
		deps = append(deps, id)
	}
	return deps, rows.Err()
}

// Ready returns open issues with no unresolved dependencies.
func (r *IssueRepo) Ready() ([]*Issue, error) {
	rows, err := r.db.Query(
		`SELECT i.id, i.issue_type, i.status, i.title, i.description, i.specialty,
		        i.branch, i.pr_number, i.assignee, i.parent_id,
		        i.created_at, i.updated_at, i.closed_at
		 FROM issues i
		 WHERE i.status = 'open'
		   AND NOT EXISTS (
		     SELECT 1 FROM issue_dependencies d
		     JOIN issues dep ON dep.id = d.depends_on_id
		     WHERE d.issue_id = i.id AND dep.status != 'closed'
		   )
		 ORDER BY i.id`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying ready issues: %w", err)
	}
	defer rows.Close()

	var issues []*Issue
	for rows.Next() {
		issue, err := scanIssueRow(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

func scanIssue(row *sql.Row) (*Issue, error) {
	var i Issue
	err := row.Scan(
		&i.ID, &i.IssueType, &i.Status, &i.Title, &i.Description,
		&i.Specialty, &i.Branch, &i.PRNumber, &i.Assignee, &i.ParentID,
		&i.CreatedAt, &i.UpdatedAt, &i.ClosedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("issue not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scanning issue: %w", err)
	}
	return &i, nil
}

func scanIssueRow(rows *sql.Rows) (*Issue, error) {
	var i Issue
	err := rows.Scan(
		&i.ID, &i.IssueType, &i.Status, &i.Title, &i.Description,
		&i.Specialty, &i.Branch, &i.PRNumber, &i.Assignee, &i.ParentID,
		&i.CreatedAt, &i.UpdatedAt, &i.ClosedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scanning issue row: %w", err)
	}
	return &i, nil
}
