-- No-op placeholder. 'under_review' is restored as a distinct valid status
-- (nc-327 reversal of nc-318 collapse). The issues.status column is already
-- VARCHAR(30) with no CHECK constraint. No DDL change is required; the
-- Go-side validation in repo.ValidStatuses is the sole gate.
SELECT 1;
