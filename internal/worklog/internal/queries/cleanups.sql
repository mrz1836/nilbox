-- name: InsertCleanup :exec
-- InsertCleanup stores one cleanup.
INSERT INTO worktree_cleanups (
    cleaned_at, automatic, repo, worktree, display_path, deleted_branch, head,
    item_kind, item_owner, item_name, item_number, item_title, item_url, item_state
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: TrimCleanups :exec
-- TrimCleanups drops every cleanup but the newest keep. Cleanups at the same time keep the
-- ones recorded last.
DELETE FROM worktree_cleanups WHERE id NOT IN (
    SELECT id FROM worktree_cleanups ORDER BY cleaned_at DESC, id DESC LIMIT sqlc.arg(keep)
);

-- name: RecentCleanups :many
-- RecentCleanups returns up to row_limit cleanups, newest first; cleanups at the same time come
-- last recorded first. A negative row_limit returns them all.
SELECT id, cleaned_at, automatic, repo, worktree, display_path, deleted_branch, head,
    item_kind, item_owner, item_name, item_number, item_title, item_url, item_state
FROM worktree_cleanups
ORDER BY cleaned_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: ClearCleanups :exec
-- ClearCleanups removes every cleanup.
DELETE FROM worktree_cleanups;
