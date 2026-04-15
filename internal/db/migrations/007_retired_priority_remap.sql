-- Retired migration. Originally remapped old P3 → new P5 and old P2 → new P3,
-- but that was unnecessary: the priority column is a plain VARCHAR with no
-- CHECK constraint (see migration 006), so expanding the valid priority set
-- from P0-P3 to P0-P5 required only a Go-side validation change. The original
-- remap also ran two statements from one file, which Dolt's database/sql
-- driver does not support. See PR #206.
--
-- Kept as a no-op placeholder so the migration sequence has no gap.
SELECT 1;
