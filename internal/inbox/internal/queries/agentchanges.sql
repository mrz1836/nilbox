-- name: UpsertAgentChanges :exec
-- UpsertAgentChanges stores what agent runs changed on an item at one head, replacing the
-- row stored before, whose paths the caller has already added in.
INSERT INTO item_agent_changes (item_id, head_oid, files, total, complete, run_id, noted_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (item_id, head_oid) DO UPDATE SET
    files = excluded.files,
    total = excluded.total,
    complete = excluded.complete,
    run_id = excluded.run_id,
    noted_at = excluded.noted_at;

-- name: GetAgentChanges :one
-- GetAgentChanges returns what agent runs changed on an item at one head.
SELECT files, total, complete
FROM item_agent_changes
WHERE item_id = ? AND head_oid = ?;

-- name: PruneAgentChanges :exec
-- PruneAgentChanges keeps an item's newest heads, by when they were noted.
DELETE FROM item_agent_changes
WHERE (item_id, head_oid) IN (
    SELECT k.item_id, k.head_oid FROM item_agent_changes k
    WHERE k.item_id = sqlc.arg(item_id)
    ORDER BY k.noted_at DESC, k.head_oid
    LIMIT -1 OFFSET sqlc.arg(row_offset)
);
