-- name: InsertNotification :one
-- InsertNotification stores one notification and returns it as stored, with its id.
INSERT INTO notifications (
    kind, created_at, item_id, item_kind, repo_owner, repo_name, number, title, url, author, detail
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id, kind, created_at, read_at, item_id, item_kind, repo_owner, repo_name, number, title, url,
    author, detail;

-- name: TrimNotifications :exec
-- TrimNotifications drops every notification but the newest keep. Notifications at the
-- same time keep the ones recorded last.
DELETE FROM notifications WHERE id NOT IN (
    SELECT id FROM notifications ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(keep)
);

-- name: ListNotifications :many
-- ListNotifications returns up to row_limit notifications, newest first; notifications at the
-- same time come last recorded first.
SELECT id, kind, created_at, read_at, item_id, item_kind, repo_owner, repo_name, number, title, url,
    author, detail
FROM notifications
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: MarkAllRead :exec
-- MarkAllRead marks every unread notification read at read_at.
UPDATE notifications SET read_at = sqlc.arg(read_at) WHERE read_at IS NULL;

-- name: ClearNotifications :exec
-- ClearNotifications removes every notification.
DELETE FROM notifications;
