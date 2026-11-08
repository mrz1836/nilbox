-- name: ListProfiles :many
-- ListProfiles returns every stored profile, in the order they were stored.
SELECT id, name, config_dir, model, effort, permission_mode, plan_mode, add_dirs, extra_args, env,
    created_at, updated_at
FROM claude_profiles
ORDER BY id;

-- name: GetProfile :one
-- GetProfile returns the profile named name, matched exactly, case included, or
-- sql.ErrNoRows when there is none.
SELECT id, name, config_dir, model, effort, permission_mode, plan_mode, add_dirs, extra_args, env,
    created_at, updated_at
FROM claude_profiles
WHERE name = ?;

-- name: InsertProfile :exec
-- InsertProfile stores a profile, unless one with its name is stored already: that one is
-- left exactly as it is.
INSERT INTO claude_profiles (
    name, config_dir, model, effort, permission_mode, plan_mode, add_dirs, extra_args, env, created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (name) DO NOTHING;

-- name: DeleteProfiles :exec
-- DeleteProfiles removes the profiles with the given ids. An empty ids matches nothing.
DELETE FROM claude_profiles WHERE id IN (sqlc.slice('ids'));
