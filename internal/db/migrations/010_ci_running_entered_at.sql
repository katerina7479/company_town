ALTER TABLE issues ADD COLUMN ci_running_entered_at TIMESTAMP NULL;
-- Backfill: rows already in ci_running at migration time get updated_at as
-- their best-available baseline. Only affects in-flight rows at upgrade moment.
UPDATE issues SET ci_running_entered_at = updated_at WHERE status = 'ci_running';
