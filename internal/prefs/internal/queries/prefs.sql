-- name: Get :one
-- Get returns the value stored under key, or sql.ErrNoRows when there is none.
SELECT value FROM prefs WHERE key = ?;

-- name: GetMany :many
-- GetMany returns the stored rows among keys, in no particular order. Keys with no row are
-- left out, and an empty keys matches nothing.
SELECT key, value FROM prefs WHERE key IN (sqlc.slice('keys'));

-- name: Set :exec
-- Set stores value under key, replacing any earlier value.
INSERT INTO prefs (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;

-- name: Delete :exec
-- Delete removes the value stored under key; a key with no value is not an error.
DELETE FROM prefs WHERE key = ?;
