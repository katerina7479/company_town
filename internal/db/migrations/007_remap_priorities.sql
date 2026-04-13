-- Remap existing priority tiers to the expanded P0–P5 scale.
-- Old scale: P0 (critical), P1 (high), P2 (medium), P3 (low)
-- New scale: P0 (critical), P1 (high), P2 (medium-high), P3 (medium), P4 (low), P5 (backlog)
-- Mapping: old P2 → new P3, old P3 → new P5 (P0 and P1 are unchanged).
-- Order matters: remap old P3 first so the subsequent P2→P3 rename does not
-- collide with tickets that were already at old P3.
UPDATE issues SET priority = 'P5' WHERE priority = 'P3';
UPDATE issues SET priority = 'P3' WHERE priority = 'P2';
