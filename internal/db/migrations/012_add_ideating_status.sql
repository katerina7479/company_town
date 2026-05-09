-- No-op placeholder. The 'ideating' status is a new valid value for the
-- issues.status column, which is already VARCHAR(30) with no CHECK constraint.
-- No DDL change is required; the Go-side validation in repo.ValidStatuses
-- is the sole gate. Kept so the migration sequence has no gap.
SELECT 1;
