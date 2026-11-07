-- name: UpsertFiles :exec
-- UpsertFiles stores the outcome of an item's file check, replacing every column of the
-- check stored before.
INSERT INTO item_files (item_id, head_oid, total, files, complete, error, checked_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (item_id) DO UPDATE SET
    head_oid = excluded.head_oid,
    total = excluded.total,
    files = excluded.files,
    complete = excluded.complete,
    error = excluded.error,
    checked_at = excluded.checked_at;

-- name: ListStoredFiles :many
-- ListStoredFiles returns the head commit and outcome of each stored file check of the
-- items of the account with login, with the item's node ID.
SELECT i.node_id, f.head_oid, f.error
FROM item_files f
JOIN items i ON i.id = f.item_id
WHERE i.account_id = (SELECT id FROM accounts WHERE login = ?)
  AND i.node_id <> '';

-- name: PruneFiles :exec
-- PruneFiles removes the file checks of the items of the account with account_id that
-- left every list.
DELETE FROM item_files WHERE item_id IN (
    SELECT id FROM items
    WHERE account_id = ? AND reason_review_requested = 0 AND reason_authored = 0 AND reason_assigned = 0
);
