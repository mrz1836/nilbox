-- name: ListUserScripts :many
-- ListUserScripts returns every script the user saved, in the order they put them in.
SELECT id, name, command, icon, scope, scope_repos, runs_in, confirm, position, created_at, updated_at
FROM user_scripts
ORDER BY position, id;

-- name: GetUserScript :one
-- GetUserScript returns one saved script, or sql.ErrNoRows when it was deleted.
SELECT id, name, command, icon, scope, scope_repos, runs_in, confirm, position, created_at, updated_at
FROM user_scripts
WHERE id = sqlc.arg(id);

-- name: InsertUserScript :one
-- InsertUserScript saves a new script at a place in the order, and returns its ID.
INSERT INTO user_scripts (
    name, command, icon, scope, scope_repos, runs_in, confirm, position, created_at, updated_at
) VALUES (
    sqlc.arg(name), sqlc.arg(command), sqlc.arg(icon), sqlc.arg(scope), sqlc.arg(scope_repos), sqlc.arg(runs_in),
    sqlc.arg(confirm), sqlc.arg(position), sqlc.arg(created_at), sqlc.arg(updated_at)
)
RETURNING id;

-- name: UpdateUserScript :execrows
-- UpdateUserScript saves a script's fields as edited, leaving its place in the order.
UPDATE user_scripts SET
    name = sqlc.arg(name),
    command = sqlc.arg(command),
    icon = sqlc.arg(icon),
    scope = sqlc.arg(scope),
    scope_repos = sqlc.arg(scope_repos),
    runs_in = sqlc.arg(runs_in),
    confirm = sqlc.arg(confirm),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: SetUserScriptPosition :exec
-- SetUserScriptPosition puts a script at a place in the order.
UPDATE user_scripts SET position = sqlc.arg(position) WHERE id = sqlc.arg(id);

-- name: DeleteUserScript :execrows
-- DeleteUserScript deletes a saved script.
DELETE FROM user_scripts WHERE id = sqlc.arg(id);
