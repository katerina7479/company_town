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
| `ADD COLUMN` | `ALTER TABLE t ADD COLUMN col …` — guard with a schema check if needed |
| `DROP COLUMN` | `ALTER TABLE t DROP COLUMN col` — guard with a schema check if needed |
| `CREATE INDEX` | `CREATE INDEX IF NOT EXISTS` |
| Seed `INSERT` | `INSERT IGNORE …` or `INSERT … ON DUPLICATE KEY UPDATE` |

**Note:** Dolt does NOT support `IF NOT EXISTS` / `IF EXISTS` on `ALTER TABLE` (tested on
Dolt 1.83). Both `ADD COLUMN IF NOT EXISTS` and `DROP COLUMN IF EXISTS` produce a parse
error. Standard MySQL also does not support these forms. Use plain `ADD COLUMN` /
`DROP COLUMN` and, if true idempotency is required, gate the statement on an explicit
schema check (e.g. query `INFORMATION_SCHEMA.COLUMNS` before running the migration).
