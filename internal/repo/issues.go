package repo

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/katerina7479/company_town/internal/eventlog"
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
	Priority    sql.NullString
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ClosedAt    sql.NullTime
}

// Valid priority values.
var ValidPriorities = []string{"P0", "P1", "P2", "P3", "P4", "P5"}

// Valid issue statuses.
var ValidStatuses = []string{
	"draft", "open", "in_progress",
	"ci_running",
	"in_review", "under_review", "pr_open",
	"reviewed", "repairing", "on_hold", "merge_conflict", "closed",
}

// Valid issue types.
var ValidTypes = []string{"task", "epic", "bug", "refactor"}

type IssueRepo struct {
	db     *sql.DB
	events *eventlog.Logger
}

func NewIssueRepo(db *sql.DB, events *eventlog.Logger) *IssueRepo {
	return &IssueRepo{db: db, events: events}
}

// Create inserts a new issue and returns its ID.
func (r *IssueRepo) Create(title, issueType string, parentID *int, specialty *string, priority *string) (int, error) {
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
	var prioVal interface{}
	if priority != nil {
		prioVal = *priority
	}

	result, err := r.db.Exec(
		`INSERT INTO issues (title, issue_type, parent_id, specialty, priority) VALUES (?, ?, ?, ?, ?)`,
		title, issueType, parentVal, specVal, prioVal,
	)
	if err != nil {
		return 0, fmt.Errorf("creating issue: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting issue id: %w", err)
	}

	if r.events != nil {
		r.events.TicketCreated(int(id), title)
	}
	return int(id), nil
}

// Get retrieves a single issue by ID.
func (r *IssueRepo) Get(id int) (*Issue, error) {
	row := r.db.QueryRow(
		`SELECT id, issue_type, status, title, description, specialty, branch,
		        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at
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
			        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at
			 FROM issues WHERE status = ? ORDER BY id`, status,
		)
	} else {
		rows, err = r.db.Query(
			`SELECT id, issue_type, status, title, description, specialty, branch,
			        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at
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
	var oldStatus string
	if r.events != nil {
		r.db.QueryRow(`SELECT status FROM issues WHERE id = ?`, id).Scan(&oldStatus) //nolint:errcheck // event pre-read; scan failure is non-fatal
	}

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

	if r.events != nil {
		r.events.TicketStatus(id, oldStatus, status)
	}
	return nil
}

// Assign sets the assignee and branch on an issue. It does NOT change the
// ticket status. To claim the work, run `gt ticket status <id> in_progress`,
// which atomically sets both the ticket status and the agent to working.
func (r *IssueRepo) Assign(id int, assignee, branch string) error {
	result, err := r.db.Exec(
		`UPDATE issues SET assignee = ?, branch = ?,
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
	r.db.Exec(`DELETE FROM issue_dependencies WHERE issue_id = ? OR depends_on_id = ?`, id, id) //nolint:errcheck // best-effort cascade delete before main delete

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

// SetAssignee sets the assignee on an issue without changing its status.
func (r *IssueRepo) SetAssignee(id int, assignee string) error {
	result, err := r.db.Exec(
		`UPDATE issues SET assignee = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		assignee, id,
	)
	if err != nil {
		return fmt.Errorf("setting issue assignee: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue %d not found", id)
	}
	return nil
}

// ClearAssignee removes the assignee from an issue.
func (r *IssueRepo) ClearAssignee(id int) error {
	result, err := r.db.Exec(
		`UPDATE issues SET assignee = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("clearing issue assignee: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue %d not found", id)
	}
	return nil
}

// BusyAssignees returns the set of agent names currently holding at least one
// ticket that requires active work. Tickets in handoff statuses (ci_running,
// in_review, under_review, pr_open, merge_conflict) are excluded: the prole
// has handed off the PR and is free to be paired with another ticket while CI
// runs or the reviewer works. ci_running is excluded because the prole is idle
// waiting for CI; if CI fails, the daemon will reassign them via repairing.
func (r *IssueRepo) BusyAssignees() (map[string]bool, error) {
	rows, err := r.db.Query(
		`SELECT DISTINCT assignee FROM issues
		 WHERE assignee IS NOT NULL
		   AND assignee != ''
		   AND status NOT IN ('closed', 'ci_running', 'in_review', 'under_review', 'pr_open', 'merge_conflict')`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying busy assignees: %w", err)
	}
	defer rows.Close()

	busy := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning busy assignee: %w", err)
		}
		busy[name] = true
	}
	return busy, rows.Err()
}

// ClearAssigneeByAgent clears the assignee on every open, in_progress,
// repairing, or ci_running issue currently assigned to `name`. in_progress
// tickets revert to open (the prole never really started). repairing and
// ci_running tickets retain their status — the review/CI loop will finish on
// its own and re-enter the pool as needed. Used during dead-prole reconcile —
// the prole row is about to be deleted, its work needs to return to the pool.
func (r *IssueRepo) ClearAssigneeByAgent(name string) (int, error) {
	result, err := r.db.Exec(
		`UPDATE issues
		 SET assignee = NULL,
		     status = CASE WHEN status = 'in_progress' THEN 'open' ELSE status END,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE assignee = ?
		   AND status IN ('open', 'in_progress', 'repairing', 'ci_running')`,
		name,
	)
	if err != nil {
		return 0, fmt.Errorf("clearing assignee for %s: %w", name, err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// UpdateDescription sets the description on an issue.
func (r *IssueRepo) UpdateDescription(id int, description string) error {
	result, err := r.db.Exec(
		`UPDATE issues SET description = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		description, id,
	)
	if err != nil {
		return fmt.Errorf("updating issue description: %w", err)
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
		        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at
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

// ListMissingPR returns non-closed issues that have a branch set but no pr_number.
func (r *IssueRepo) ListMissingPR() ([]*Issue, error) {
	rows, err := r.db.Query(
		`SELECT id, issue_type, status, title, description, specialty, branch,
		        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at
		 FROM issues
		 WHERE pr_number IS NULL AND branch IS NOT NULL AND status != 'closed'
		 ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing issues missing PR: %w", err)
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

// RemoveDependency removes the dependency edge where issueID depends on dependsOnID.
// It is idempotent: removing a non-existent edge succeeds silently.
func (r *IssueRepo) RemoveDependency(issueID, dependsOnID int) error {
	_, err := r.db.Exec(
		`DELETE FROM issue_dependencies WHERE issue_id = ? AND depends_on_id = ?`,
		issueID, dependsOnID,
	)
	if err != nil {
		return fmt.Errorf("removing dependency: %w", err)
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

// Ready returns open issues with no unresolved dependencies, ordered by priority (P0 first, NULL last).
func (r *IssueRepo) Ready() ([]*Issue, error) {
	rows, err := r.db.Query(
		`SELECT i.id, i.issue_type, i.status, i.title, i.description, i.specialty,
		        i.branch, i.pr_number, i.assignee, i.parent_id, i.priority,
		        i.created_at, i.updated_at, i.closed_at
		 FROM issues i
		 WHERE i.status = 'open'
		   AND i.issue_type != 'epic'
		   AND NOT EXISTS (
		     SELECT 1 FROM issue_dependencies d
		     JOIN issues dep ON dep.id = d.depends_on_id
		     WHERE d.issue_id = i.id AND dep.status != 'closed'
		   )
		 ORDER BY CASE
		   WHEN i.priority = 'P0' THEN 0
		   WHEN i.priority = 'P1' THEN 1
		   WHEN i.priority = 'P2' THEN 2
		   WHEN i.priority = 'P3' THEN 3
		   WHEN i.priority = 'P4' THEN 4
		   WHEN i.priority = 'P5' THEN 5
		   ELSE 6
		 END, i.id`,
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

// Selectable returns unassigned tickets that are ready for daemon-driven
// assignment. Selection includes:
//   - repairing tickets with no assignee (orphaned — prole died before fixing)
//   - open tickets with no unmet dependencies and no assignee
//
// Ordering (strict): repairing before open, bugs before tasks before other
// types, P0→P1→P2→P3→P4→P5→null, then lower ID first.
//
// Note: once nc-41 lands (proles stay assigned through repairing), the
// repairing branch returns nothing for tickets whose prole is still alive.
// Selectable() only catches orphaned repairing tickets.
func (r *IssueRepo) Selectable() ([]*Issue, error) {
	rows, err := r.db.Query(
		`SELECT i.id, i.issue_type, i.status, i.title, i.description, i.specialty,
		        i.branch, i.pr_number, i.assignee, i.parent_id, i.priority,
		        i.created_at, i.updated_at, i.closed_at
		 FROM issues i
		 WHERE i.issue_type != 'epic'
		   AND (
		         i.status = 'repairing'
		      OR (
		           i.status = 'open'
		           AND NOT EXISTS (
		             SELECT 1 FROM issue_dependencies d
		             JOIN issues dep ON dep.id = d.depends_on_id
		             WHERE d.issue_id = i.id AND dep.status != 'closed'
		           )
		         )
		   )
		   AND (i.assignee IS NULL OR i.assignee = '')
		 ORDER BY
		   CASE i.status WHEN 'repairing' THEN 0 ELSE 1 END,
		   CASE i.issue_type WHEN 'bug' THEN 0 WHEN 'task' THEN 1 ELSE 2 END,
		   CASE i.priority
		     WHEN 'P0' THEN 0
		     WHEN 'P1' THEN 1
		     WHEN 'P2' THEN 2
		     WHEN 'P3' THEN 3
		     WHEN 'P4' THEN 4
		     WHEN 'P5' THEN 5
		     ELSE 6
		   END,
		   i.id`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying selectable issues: %w", err)
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

// ListAssignedInStatuses returns issues whose status is one of the given values
// AND that have a non-empty assignee. The result is ordered by id.
// Passing no statuses returns an empty slice.
func (r *IssueRepo) ListAssignedInStatuses(statuses ...string) ([]*Issue, error) {
	if len(statuses) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(statuses))
	args := make([]interface{}, len(statuses))
	for i, s := range statuses {
		placeholders[i] = "?"
		args[i] = s
	}

	//nolint:gosec // G202: placeholders are parameterized ?s generated from len(statuses), not user input
	query := `SELECT id, issue_type, status, title, description, specialty, branch,
	                 pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at
	          FROM issues
	          WHERE status IN (` + strings.Join(placeholders, ", ") + `)
	            AND assignee IS NOT NULL AND assignee != ''
	          ORDER BY id`

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing assigned issues in statuses: %w", err)
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

// ListEpicsWithAllChildrenClosed returns epics that are not closed but have at
// least one child and all children are closed.
func (r *IssueRepo) ListEpicsWithAllChildrenClosed() ([]*Issue, error) {
	rows, err := r.db.Query(
		`SELECT id, issue_type, status, title, description, specialty, branch,
		        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at
		 FROM issues
		 WHERE issue_type = 'epic'
		   AND status != 'closed'
		   AND EXISTS (
		     SELECT 1 FROM issues child WHERE child.parent_id = issues.id
		   )
		   AND NOT EXISTS (
		     SELECT 1 FROM issues child WHERE child.parent_id = issues.id AND child.status != 'closed'
		   )
		 ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing epics with all children closed: %w", err)
	}
	defer rows.Close()

	var epics []*Issue
	for rows.Next() {
		issue, err := scanIssueRow(rows)
		if err != nil {
			return nil, err
		}
		epics = append(epics, issue)
	}
	return epics, rows.Err()
}

// IssueNode wraps an Issue with its children for hierarchical display.
type IssueNode struct {
	*Issue
	Children []*IssueNode
}

// ListHierarchy returns all issues organized as a forest (slice of root nodes).
// Issues without a parent_id are roots; others are nested under their parent.
// Issues whose parent is not found in the result set are treated as roots.
func (r *IssueRepo) ListHierarchy() ([]*IssueNode, error) {
	issues, err := r.List("")
	if err != nil {
		return nil, fmt.Errorf("listing issues for hierarchy: %w", err)
	}

	nodes := make(map[int]*IssueNode, len(issues))
	for _, issue := range issues {
		nodes[issue.ID] = &IssueNode{Issue: issue}
	}

	var roots []*IssueNode
	for _, issue := range issues {
		node := nodes[issue.ID]
		if !issue.ParentID.Valid {
			roots = append(roots, node)
		} else {
			parentID := int(issue.ParentID.Int64)
			if parent, ok := nodes[parentID]; ok {
				parent.Children = append(parent.Children, node)
			} else {
				roots = append(roots, node)
			}
		}
	}

	return roots, nil
}

// SetPriority sets the priority on an issue.
func (r *IssueRepo) SetPriority(id int, priority string) error {
	result, err := r.db.Exec(
		`UPDATE issues SET priority = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		priority, id,
	)
	if err != nil {
		return fmt.Errorf("setting issue priority: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue %d not found", id)
	}
	return nil
}

func (r *IssueRepo) UpdateType(id int, issueType string) error {
	result, err := r.db.Exec(
		`UPDATE issues SET issue_type = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		issueType, id,
	)
	if err != nil {
		return fmt.Errorf("updating issue type: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue %d not found", id)
	}
	return nil
}

func scanIssue(row *sql.Row) (*Issue, error) {
	var i Issue
	err := row.Scan(
		&i.ID, &i.IssueType, &i.Status, &i.Title, &i.Description,
		&i.Specialty, &i.Branch, &i.PRNumber, &i.Assignee, &i.ParentID,
		&i.Priority, &i.CreatedAt, &i.UpdatedAt, &i.ClosedAt,
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
		&i.Priority, &i.CreatedAt, &i.UpdatedAt, &i.ClosedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scanning issue row: %w", err)
	}
	return &i, nil
}

// SetParent sets the parent_id of issue id to parentID.
// Returns an error if the issue does not exist.
func (r *IssueRepo) SetParent(id, parentID int) error {
	result, err := r.db.Exec(
		`UPDATE issues SET parent_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		parentID, id,
	)
	if err != nil {
		return fmt.Errorf("setting parent: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue %d not found", id)
	}
	return nil
}

// ClearParent removes the parent_id from the issue, making it a root-level ticket.
// Returns an error if the issue does not exist.
func (r *IssueRepo) ClearParent(id int) error {
	result, err := r.db.Exec(
		`UPDATE issues SET parent_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("clearing parent: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue %d not found", id)
	}
	return nil
}
