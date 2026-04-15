-- Remap old P3 tickets to new P5. Must run before 007b which remaps old P2 → new P3.
-- Split across two files because Dolt's database/sql driver does not accept
-- multi-statement queries via db.Exec.
-- Old scale: P0 (critical), P1 (high), P2 (medium), P3 (low)
-- New scale: P0 (critical), P1 (high), P2 (medium-high), P3 (medium), P4 (low), P5 (backlog)
UPDATE issues SET priority = 'P5' WHERE priority = 'P3';
