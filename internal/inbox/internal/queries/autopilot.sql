-- name: ListItemStates :many
-- ListItemStates returns, for each stored item among ids, its repository, kind, and
-- number, its state as the last read that carries it said, when a read last brought it,
-- whether it is in one of the inbox's lists, and whether a routine's search watches it:
-- what Autopilot's engine reads before it closes or prunes what it recorded of an item.
-- An id with no stored item is left out.
SELECT i.id, i.kind, i.number, r.owner AS repo_owner, r.name AS repo_name, i.state, i.fetched_at,
    CAST(i.reason_review_requested = 1 OR i.reason_authored = 1 OR i.reason_assigned = 1 OR i.reason_pushed = 1 AS BOOLEAN) AS listed,
    CAST(EXISTS (SELECT 1 FROM item_watches w WHERE w.item_id = i.id) AS BOOLEAN) AS watched
FROM items i
JOIN repos r ON r.id = i.repo_id
WHERE i.id IN (sqlc.slice(ids));
