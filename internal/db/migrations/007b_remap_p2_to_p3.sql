-- Second half of the P2→P3 / P3→P5 remap begun in 007_remap_priorities.sql.
-- Must run after 007 so tickets already at old P3 are moved to P5 before the
-- old P2 → new P3 rename runs.
UPDATE issues SET priority = 'P3' WHERE priority = 'P2';
