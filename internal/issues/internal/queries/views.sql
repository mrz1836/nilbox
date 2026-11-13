-- name: GetView :one
-- GetView returns the cached view of the item with item_id, or sql.ErrNoRows when the sheet
-- never read it.
SELECT item_id, view, bytes, fetched_at, used_at, seen_at
FROM issue_views
WHERE item_id = ?;

-- name: UpsertView :exec
-- UpsertView stores what the sheet read about the item with item_id, replacing what was
-- cached, and leaves seen_at, which only OpenView moves, as it was.
INSERT INTO issue_views (item_id, view, bytes, fetched_at, used_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (item_id) DO UPDATE SET
    view = excluded.view,
    bytes = excluded.bytes,
    fetched_at = excluded.fetched_at,
    used_at = excluded.used_at;

-- name: OpenView :exec
-- OpenView stamps the sheet's opening of the item with item_id: the read it shows, by
-- GitHub's time of its newest activity, and when.
UPDATE issue_views SET seen_at = ?, used_at = ? WHERE item_id = ?;

-- name: ExpireView :exec
-- ExpireView marks the cached view of the item with item_id out of date, so the next refresh
-- reads it.
UPDATE issue_views SET fetched_at = NULL WHERE item_id = ?;

-- name: DeleteView :exec
-- DeleteView drops the cached view of the item with item_id.
DELETE FROM issue_views WHERE item_id = ?;

-- name: PruneUnusedViews :execrows
-- PruneUnusedViews drops the views the sheet has not opened since before.
DELETE FROM issue_views WHERE used_at < ?;

-- name: ListViewUsage :many
-- ListViewUsage returns every cached view's size, the most recently used first.
SELECT item_id, bytes FROM issue_views ORDER BY used_at DESC, item_id DESC;
