package repo

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/katerina7479/company_town/internal/eventlog"
)

type Issue struct {
	ID               int
	IssueType        string
	Status           string
	Title            string
	Description      sql.NullString
	Specialty        sql.NullString
	Branch           sql.NullString
	PRNumber         sql.NullInt64
	Assignee         sql.NullString
	ParentID         sql.NullInt64
	Priority         sql.NullString
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ClosedAt         sql.NullTime
	RepairCycleCount int
	RepairReason     sql.NullString
}

// Valid priority values.
var ValidPriorities = []string{"P0", "P1", "P2", "P3", "P4", "P5"}

// Valid issue statuses.
var ValidStatuses = []string{
	StatusDraft, StatusOpen, StatusInProgress,
	StatusCIRunning,
	StatusInReview, StatusUnderReview, StatusPROpen,
	StatusReviewed, StatusRepairing, StatusOnHold, StatusMergeConflict, StatusClosed, StatusCancelled,
}

// Valid issue types.
var ValidTypes = []string{"task", "epic", "bug", "refactor", "tdd_tests"}

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
		        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at,
		        repair_cycle_count, repair_reason
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
			        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at,
			        repair_cycle_count, repair_reason
			 FROM issues WHERE status = ? ORDER BY id`, status,
		)
	} else {
		rows, err = r.db.Query(
			`SELECT id, issue_type, status, title, description, specialty, branch,
			        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at,
			        repair_cycle_count, repair_reason
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

// UpdateStatus changes an issue's status. When transitioning to "repairing",
// repair_cycle_count is incremented atomically so the daemon can detect
// tickets that have bounced too many times and escalate them. When
// transitioning to "open", repair_cycle_count is reset to 0 so a human
// unblocking an on_hold ticket gets a fresh slate. When transitioning out of
// a repair-ish state (to draft, in_progress, etc.), repair_reason is cleared
// so stale messages don't linger on recovered tickets.
func (r *IssueRepo) UpdateStatus(id int, status string) error {
	var oldStatus string
	if r.events != nil {
		r.db.QueryRow(`SELECT status FROM issues WHERE id = ?`, id).Scan(&oldStatus) //nolint:errcheck // event pre-read; scan failure is non-fatal
	}

	var closedAt interface{}
	if status == StatusClosed || status == StatusCancelled {
		closedAt = time.Now()
	}

	// b collects the SET clause so that common columns are specified once.
	// Adding a new always-updated column means one line here, not one per case.
	b := newSetBuilder().
		set("status = ?", status).
		set("closed_at = ?", closedAt).
		expr("updated_at = CURRENT_TIMESTAMP")

	switch status {
	case StatusRepairing:
		b.expr("repair_cycle_count = repair_cycle_count + 1")
	case StatusOpen:
		// Human unblock: reset repair_cycle_count so the ticket gets a fresh
		// slate. Also clear repair_reason in case it was on_hold.
		b.expr("repair_cycle_count = 0").expr("repair_reason = NULL")
	case StatusDraft, StatusInProgress, StatusCIRunning, StatusInReview, StatusUnderReview, StatusPROpen, StatusClosed, StatusCancelled, StatusOnHold:
		// Transitioning out of a repair-ish state — clear stale repair_reason.
		// "draft" is included because a human may manually reopen a ticket from
		// on_hold or repairing back to draft, and the old reason must not leak.
		b.expr("repair_reason = NULL")
	}

	setSQL, setArgs := b.build()
	result, err := r.db.Exec(
		"UPDATE issues SET "+setSQL+" WHERE id = ?", //nolint:gosec // G202: setSQL is built from string literals only, never user input
		append(setArgs, id)...,
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

// ClearBranch sets branch to NULL on an issue.
func (r *IssueRepo) ClearBranch(id int) error {
	result, err := r.db.Exec(
		`UPDATE issues SET branch = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("clearing issue branch: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("issue %d not found", id)
	}
	return nil
}

// ClearPR sets pr_number to NULL on an issue.
func (r *IssueRepo) ClearPR(id int) error {
	result, err := r.db.Exec(
		`UPDATE issues SET pr_number = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("clearing issue PR number: %w", err)
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

// ListWithPRs returns non-terminal issues that have a PR number set.
func (r *IssueRepo) ListWithPRs() ([]*Issue, error) {
	rows, err := r.db.Query(
		`SELECT id, issue_type, status, title, description, specialty, branch,
		        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at,
		        repair_cycle_count, repair_reason
		 FROM issues WHERE pr_number IS NOT NULL AND status NOT IN ('closed', 'cancelled')
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

// ListMissingPR returns non-terminal issues that have a branch set but no pr_number.
func (r *IssueRepo) ListMissingPR() ([]*Issue, error) {
	rows, err := r.db.Query(
		`SELECT id, issue_type, status, title, description, specialty, branch,
		        pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at,
		        repair_cycle_count, repair_reason
		 FROM issues
		 WHERE pr_number IS NULL AND branch IS NOT NULL AND status NOT IN ('closed', 'cancelled')
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
	return r.UpdateStatus(id, StatusClosed)
}

// SetBranch sets the branch field on an issue.
func (r *IssueRepo) SetBranch(id int, branch string) error {
	_, err := r.db.Exec(
		`UPDATE issues SET branch = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		branch, id,
	)
	return err
}

// GetDependents returns issues whose work depends on the given issue ID — that
// is, issues where a row exists in issue_dependencies with depends_on_id equal
// to the given ID. Only non-terminal (non-closed, non-cancelled) dependents are returned.
func (r *IssueRepo) GetDependents(dependsOnID int) ([]*Issue, error) {
	rows, err := r.db.Query(
		`SELECT i.id, i.issue_type, i.status, i.title, i.description, i.specialty,
		        i.branch, i.pr_number, i.assignee, i.parent_id, i.priority,
		        i.created_at, i.updated_at, i.closed_at, i.repair_cycle_count, i.repair_reason
		 FROM issues i
		 JOIN issue_dependencies d ON d.issue_id = i.id
		 WHERE d.depends_on_id = ? AND i.status NOT IN ('closed', 'cancelled')
		 ORDER BY i.id`,
		dependsOnID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting dependents: %w", err)
	}
	defer rows.Close() //nolint:errcheck

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
		        i.created_at, i.updated_at, i.closed_at, i.repair_cycle_count, i.repair_reason
		 FROM issues i
		 WHERE i.status = 'open'
		   AND i.issue_type != 'epic'
		   AND NOT EXISTS (
		     SELECT 1 FROM issue_dependencies d
		     JOIN issues dep ON dep.id = d.depends_on_id
		     WHERE d.issue_id = i.id AND dep.status NOT IN ('closed', 'cancelled')
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
		// ancestor_chain maps every issue to each of its transitive ancestors.
		// Used to check whether any ancestor has unclosed dependencies that would
		// block a descendant from being selected. Single parent_id means the graph
		// is a forest; diamond ancestry is not possible with today's data model.
		`WITH RECURSIVE ancestor_chain(issue_id, ancestor_id) AS (
		   SELECT id AS issue_id, parent_id AS ancestor_id
		   FROM issues
		   WHERE parent_id IS NOT NULL
		   UNION ALL
		   SELECT ac.issue_id, p.parent_id AS ancestor_id
		   FROM ancestor_chain ac
		   JOIN issues p ON p.id = ac.ancestor_id
		   WHERE p.parent_id IS NOT NULL
		 )
		 SELECT i.id, i.issue_type, i.status, i.title, i.description, i.specialty,
		        i.branch, i.pr_number, i.assignee, i.parent_id, i.priority,
		        i.created_at, i.updated_at, i.closed_at, i.repair_cycle_count, i.repair_reason
		 FROM issues i
		 WHERE i.issue_type != 'epic'
		   AND (
		         i.status = 'repairing'
		      OR (
		           i.status = 'open'
		           AND NOT EXISTS (
		             SELECT 1 FROM issue_dependencies d
		             JOIN issues dep ON dep.id = d.depends_on_id
		             WHERE d.issue_id = i.id AND dep.status NOT IN ('closed', 'cancelled')
		           )
		           AND NOT EXISTS (
		             SELECT 1 FROM ancestor_chain ac
		             JOIN issue_dependencies ad ON ad.issue_id = ac.ancestor_id
		             JOIN issues dep ON dep.id = ad.depends_on_id
		             WHERE ac.issue_id = i.id AND dep.status NOT IN ('closed', 'cancelled')
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
	                 pr_number, assignee, parent_id, priority, created_at, updated_at, closed_at,
	                 repair_cycle_count, repair_reason
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

// ListEpicsWithAllChildrenClosed returns epics that are not terminal but have
// at least one transitive descendant and all transitive descendants are
// terminal (closed or cancelled). Uses a recursive CTE so nested epics
// (Epic → Sub-Epic → Tasks) auto-close as a unit rather than requiring each
// level to close independently tick-by-tick. Cancelled is treated as terminal
// per nc-263.
func (r *IssueRepo) ListEpicsWithAllChildrenClosed() ([]*Issue, error) {
	rows, err := r.db.Query(
		`WITH RECURSIVE descendants(epic_id, descendant_id, descendant_status) AS (
		   SELECT epic.id, child.id, child.status
		   FROM issues epic
		   JOIN issues child ON child.parent_id = epic.id
		   WHERE epic.issue_type = 'epic' AND epic.status NOT IN ('closed', 'cancelled')
		   UNION ALL
		   SELECT d.epic_id, grandchild.id, grandchild.status
		   FROM descendants d
		   JOIN issues grandchild ON grandchild.parent_id = d.descendant_id
		 )
		 SELECT e.id, e.issue_type, e.status, e.title, e.description, e.specialty, e.branch,
		        e.pr_number, e.assignee, e.parent_id, e.priority, e.created_at, e.updated_at,
		        e.closed_at, e.repair_cycle_count, e.repair_reason
		 FROM issues e
		 WHERE e.issue_type = 'epic'
		   AND e.status NOT IN ('closed', 'cancelled')
		   AND EXISTS (SELECT 1 FROM descendants d WHERE d.epic_id = e.id)
		   AND NOT EXISTS (SELECT 1 FROM descendants d WHERE d.epic_id = e.id AND d.descendant_status NOT IN ('closed', 'cancelled'))
		 ORDER BY e.id`,
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
	Children             []*IssueNode
	UnmetDeps            []*Issue   // unclosed direct dependencies
	BlockingAncestorNode *IssueNode // nearest ancestor with unclosed deps (nil if none)
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

// ListHierarchyWithDeps returns all issues as a forest with dependency
// information attached to each node. UnmetDeps lists unclosed direct
// dependencies; BlockingAncestorNode points to the nearest ancestor that has
// unclosed deps of its own (propagated top-down after the tree is built).
func (r *IssueRepo) ListHierarchyWithDeps() ([]*IssueNode, error) {
	issues, err := r.List("")
	if err != nil {
		return nil, fmt.Errorf("listing issues for hierarchy with deps: %w", err)
	}

	nodes := make(map[int]*IssueNode, len(issues))
	for _, issue := range issues {
		nodes[issue.ID] = &IssueNode{Issue: issue}
	}

	// Load all unclosed dependencies in one query: issue_id → [dep issue, ...]
	depRows, err := r.db.Query(
		`SELECT d.issue_id, dep.id, dep.issue_type, dep.status, dep.title, dep.description,
		        dep.specialty, dep.branch, dep.pr_number, dep.assignee, dep.parent_id,
		        dep.priority, dep.created_at, dep.updated_at, dep.closed_at,
		        dep.repair_cycle_count, dep.repair_reason
		 FROM issue_dependencies d
		 JOIN issues dep ON dep.id = d.depends_on_id
		 WHERE dep.status != 'closed'
		 ORDER BY d.issue_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("loading unmet deps for hierarchy: %w", err)
	}
	defer depRows.Close() //nolint:errcheck // sql.Rows.Close never fails in practice; error surfaced via rows.Err()
	for depRows.Next() {
		var issueID int
		dep, err := scanIssueRowWithPrefix(depRows, &issueID)
		if err != nil {
			return nil, err
		}
		if node, ok := nodes[issueID]; ok {
			node.UnmetDeps = append(node.UnmetDeps, dep)
		}
	}
	if err := depRows.Err(); err != nil {
		return nil, err
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

	propagateBlockingAncestor(roots, nil)
	return roots, nil
}

// propagateBlockingAncestor traverses the tree top-down, annotating each node
// with its nearest blocking ancestor (the closest ancestor that has UnmetDeps).
func propagateBlockingAncestor(nodes []*IssueNode, inherited *IssueNode) {
	for _, n := range nodes {
		n.BlockingAncestorNode = inherited
		var nextAncestor *IssueNode
		if len(n.UnmetDeps) > 0 {
			nextAncestor = n
		} else {
			nextAncestor = inherited
		}
		propagateBlockingAncestor(n.Children, nextAncestor)
	}
}

// scanIssueRowWithPrefix scans a row that begins with an extra integer column
// (the owning issue_id from an issue_dependencies join) followed by the
// standard issue columns.
func scanIssueRowWithPrefix(row interface {
	Scan(...interface{}) error
}, issueID *int) (*Issue, error) {
	var i Issue
	err := row.Scan(
		issueID,
		&i.ID, &i.IssueType, &i.Status, &i.Title, &i.Description,
		&i.Specialty, &i.Branch, &i.PRNumber, &i.Assignee, &i.ParentID,
		&i.Priority, &i.CreatedAt, &i.UpdatedAt, &i.ClosedAt,
		&i.RepairCycleCount, &i.RepairReason,
	)
	return &i, err
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

// SetRepairReason records why a ticket entered a repair-ish state. Passing an
// empty string stores NULL (not an empty string). Call this after UpdateStatus
// moves the ticket to repairing or merge_conflict.
func (r *IssueRepo) SetRepairReason(id int, reason string) error {
	var val interface{}
	if reason != "" {
		val = reason
	}
	result, err := r.db.Exec(
		`UPDATE issues SET repair_reason = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		val, id,
	)
	if err != nil {
		return fmt.Errorf("setting repair reason: %w", err)
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
		&i.RepairCycleCount, &i.RepairReason,
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
		&i.RepairCycleCount, &i.RepairReason,
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

// setBuilder accumulates the SET clause of an UPDATE statement one column at a
// time. Using it in UpdateStatus means that columns shared by every transition
// (status, closed_at, updated_at) are written once; only the status-specific
// extras vary per case. Adding a new always-updated column is a single-line
// change instead of touching every branch.
type setBuilder struct {
	exprs []string
	args  []interface{}
}

func newSetBuilder() *setBuilder { return &setBuilder{} }

// set appends "expr" and binds arg as its ? placeholder value.
func (b *setBuilder) set(expr string, arg interface{}) *setBuilder {
	b.exprs = append(b.exprs, expr)
	b.args = append(b.args, arg)
	return b
}

// expr appends a raw SQL fragment with no bound parameter (e.g. "repair_reason = NULL").
func (b *setBuilder) expr(fragment string) *setBuilder {
	b.exprs = append(b.exprs, fragment)
	return b
}

// build returns the comma-joined SET clause and the ordered argument slice.
// The caller is responsible for appending the WHERE clause argument.
func (b *setBuilder) build() (string, []interface{}) {
	return strings.Join(b.exprs, ", "), b.args
}
