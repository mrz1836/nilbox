-- name: ListUserScripts :many
-- ListUserScripts returns every script the user saved, in the order they put them in.
SELECT id, name, command, icon, scope, scope_repos, runs_in, confirm, position, created_at, updated_at, uuid
FROM user_scripts
ORDER BY position, id;

-- name: GetUserScript :one
-- GetUserScript returns one saved script, or sql.ErrNoRows when it was deleted.
SELECT id, name, command, icon, scope, scope_repos, runs_in, confirm, position, created_at, updated_at, uuid
FROM user_scripts
WHERE id = sqlc.arg(id);

-- name: InsertUserScript :one
-- InsertUserScript saves a new script, under its UUID, at a place in the order, and returns
-- its ID.
INSERT INTO user_scripts (
    name, command, icon, scope, scope_repos, runs_in, confirm, position, created_at, updated_at, uuid
) VALUES (
    sqlc.arg(name), sqlc.arg(command), sqlc.arg(icon), sqlc.arg(scope), sqlc.arg(scope_repos), sqlc.arg(runs_in),
    sqlc.arg(confirm), sqlc.arg(position), sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.arg(uuid)
)
RETURNING id;

-- name: UpdateUserScript :one
-- UpdateUserScript saves a script's fields as edited, leaving its place in the order and its
-- UUID, which it returns; sql.ErrNoRows when the script was deleted.
UPDATE user_scripts SET
    name = sqlc.arg(name),
    command = sqlc.arg(command),
    icon = sqlc.arg(icon),
    scope = sqlc.arg(scope),
    scope_repos = sqlc.arg(scope_repos),
    runs_in = sqlc.arg(runs_in),
    confirm = sqlc.arg(confirm),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING uuid;

-- name: SetUserScriptPosition :exec
-- SetUserScriptPosition puts a script at a place in the order.
UPDATE user_scripts SET position = sqlc.arg(position) WHERE id = sqlc.arg(id);

-- name: DeleteUserScript :execrows
-- DeleteUserScript deletes a saved script.
DELETE FROM user_scripts WHERE id = sqlc.arg(id);

-- name: CountUserScriptUUID :one
-- CountUserScriptUUID counts the scripts with uuid: one means it is taken.
SELECT COUNT(*) FROM user_scripts WHERE uuid = sqlc.arg(uuid);

-- name: UserScriptsWithoutUUID :many
-- UserScriptsWithoutUUID returns the IDs of the scripts with no UUID yet, as a build from
-- before UUIDs inserts them.
SELECT id FROM user_scripts WHERE uuid = '' ORDER BY id;

-- name: SetUserScriptUUID :exec
-- SetUserScriptUUID gives a script a UUID. Nothing else changes, its updated_at included.
UPDATE user_scripts SET uuid = sqlc.arg(uuid) WHERE id = sqlc.arg(id);
