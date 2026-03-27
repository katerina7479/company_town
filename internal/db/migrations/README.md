# Migrations

Migration files are applied in filename order by `db.RunMigrations`.
Applied migrations are tracked in the `schema_migrations` table; already-applied
files are skipped on subsequent runs.

## Idempotency requirement

**Every migration MUST be safe to re-run against an existing schema.**

The tracking table guards against double-application under normal operation, but
it can be absent after a DB restore or in development. A migration that fails on
re-run will break `ct init` and `gt migrate` in those scenarios.

Follow these patterns:

| Operation | Safe form |
|-----------|-----------|
| `CREATE TABLE` | `CREATE TABLE IF NOT EXISTS` |
| `ADD COLUMN` | `ALTER TABLE t ADD COLUMN IF NOT EXISTS col …` |
| `DROP COLUMN` | `ALTER TABLE t DROP COLUMN IF EXISTS col` |
| `CREATE INDEX` | `CREATE INDEX IF NOT EXISTS` |
| Seed `INSERT` | `INSERT IGNORE …` or `INSERT … ON DUPLICATE KEY UPDATE` |

Dolt (>= 1.0) supports MySQL-compatible `IF NOT EXISTS` / `IF EXISTS` on `ALTER TABLE`.
