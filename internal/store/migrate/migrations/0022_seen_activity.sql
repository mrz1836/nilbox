-- 0022_seen_activity: the activity on an item the user last saw, so a card can show it unread.
--
-- The conventions are 0001_init's. The inbox owns every column here.

-- == items ===============================================================================

-- The inbox's cards show a dot on an item with activity on GitHub since the user last
-- opened it. seen_activity_at is the activity the user last saw, compared with
-- item_updated_at: the item is unread while it is NULL (never seen) or item_updated_at is
-- later. Only the window's marks move it (the inbox's MarkSeen, to the later of the item's
-- activity and the time of the mark, and MarkUnseen, to NULL), and a new row's insert sets
-- it: a whole refresh stores NULL, except on an account's first load, which stores the
-- item's activity, as a routine's search always does. No read rewrites it.
ALTER TABLE items ADD COLUMN seen_activity_at DATETIME;

-- Every item stored before this counts as seen, so the upgrade lights no card; what
-- arrives afterwards starts unread.
UPDATE items SET seen_activity_at = item_updated_at;
