-- name: ListAvatars :many
-- ListAvatars returns the pictures kept of the logins, each lower-cased.
SELECT login, mime, image, fetched_at, used_at
FROM avatars
WHERE login IN (sqlc.slice(logins));

-- name: UpsertAvatar :exec
-- UpsertAvatar keeps the picture just downloaded of login, replacing the one kept.
INSERT INTO avatars (login, mime, image, bytes, fetched_at, used_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (login) DO UPDATE SET
    mime = excluded.mime,
    image = excluded.image,
    bytes = excluded.bytes,
    fetched_at = excluded.fetched_at,
    used_at = excluded.used_at;

-- name: TouchAvatars :exec
-- TouchAvatars stamps the logins' pictures as shown at used_at, each last stamped at or
-- before before, so a picture shown often is written at most once a day.
UPDATE avatars
SET used_at = sqlc.arg(used_at)
WHERE used_at <= sqlc.arg(before) AND login IN (sqlc.slice(logins));

-- name: PruneUnusedAvatars :execrows
-- PruneUnusedAvatars drops the pictures no sheet has shown since before.
DELETE FROM avatars WHERE used_at < ?;

-- name: ListAvatarUsage :many
-- ListAvatarUsage returns every kept picture's size, the most recently shown first.
SELECT login, bytes FROM avatars ORDER BY used_at DESC, login DESC;

-- name: DeleteAvatar :exec
-- DeleteAvatar drops the picture of login.
DELETE FROM avatars WHERE login = ?;
