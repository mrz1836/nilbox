-- 0010_autopilot_matches_item: find an item's matches without reading every match.
--
-- The window reads the newest match of each item a row shows (its chip) and an item's
-- history whenever Autopilot changes, up to four times a second, which read the matches by
-- item alone. Autopilot owns the index.

-- == autopilot ===========================================================================

CREATE INDEX autopilot_matches_item ON autopilot_matches (item_id);
