-- name: GetDiff :one
-- GetDiff returns the cached review of the item with item_id, or sql.ErrNoRows when the
-- sheet never read it.
SELECT item_id, head_oid, total, complete, bytes, context, context_at, fetched_at, used_at
FROM review_diffs
WHERE item_id = ?;

-- name: ListDiffFiles :many
-- ListDiffFiles returns the cached files of the item with item_id, in GitHub's order, with
-- each patch's length but not its text.
SELECT position, path, previous_path, status, additions, deletions, blob_sha, patch_state,
    CAST(length(CAST(patch AS BLOB)) AS INTEGER) AS patch_bytes
FROM review_diff_files
WHERE item_id = ?
ORDER BY position;

-- name: ListDiffPatches :many
-- ListDiffPatches returns the patches of the cached files of the item with item_id at the
-- positions asked for.
SELECT position, patch, patch_state
FROM review_diff_files
WHERE item_id = sqlc.arg(item_id) AND position IN (sqlc.slice(positions))
ORDER BY position;

-- name: UpsertDiff :exec
-- UpsertDiff stores what the sheet read about the item with item_id: its files' head,
-- count, and size, and the context, replacing what was cached.
INSERT INTO review_diffs (item_id, head_oid, total, complete, bytes, context, context_at, fetched_at, used_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (item_id) DO UPDATE SET
    head_oid = excluded.head_oid,
    total = excluded.total,
    complete = excluded.complete,
    bytes = excluded.bytes,
    context = excluded.context,
    context_at = excluded.context_at,
    fetched_at = excluded.fetched_at,
    used_at = excluded.used_at;

-- name: SetDiffContext :exec
-- SetDiffContext stores a context read again for the item with item_id, whose files are
-- still current, and stamps its use.
UPDATE review_diffs SET context = ?, context_at = ?, used_at = ? WHERE item_id = ?;

-- name: DeleteDiffFiles :exec
-- DeleteDiffFiles removes the cached files of the item with item_id.
DELETE FROM review_diff_files WHERE item_id = ?;

-- name: InsertDiffFile :exec
-- InsertDiffFile caches one file of the item with item_id.
INSERT INTO review_diff_files (
    item_id, position, path, previous_path, status, additions, deletions, blob_sha, patch, patch_state
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: TouchDiff :exec
-- TouchDiff stamps the use of the cached review of the item with item_id.
UPDATE review_diffs SET used_at = ? WHERE item_id = ?;

-- name: ExpireDiffContext :exec
-- ExpireDiffContext marks the cached context of the item with item_id as read at no time,
-- so the next refresh reads it again.
UPDATE review_diffs SET context_at = NULL WHERE item_id = ?;

-- name: DeleteDiff :exec
-- DeleteDiff removes the cached review of the item with item_id, its files with it.
DELETE FROM review_diffs WHERE item_id = ?;

-- name: PruneUnusedDiffs :execrows
-- PruneUnusedDiffs removes every cached review last used before the cutoff.
DELETE FROM review_diffs WHERE used_at < ?;

-- name: ListDiffUsage :many
-- ListDiffUsage lists every cached review's item and size, the most recently used first.
SELECT item_id, bytes FROM review_diffs ORDER BY used_at DESC, item_id DESC;
