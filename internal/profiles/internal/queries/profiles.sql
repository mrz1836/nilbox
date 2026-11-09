-- name: ListProfiles :many
-- ListProfiles returns every stored profile of every agent, in the order they were stored.
SELECT id, agent, name, config_dir, model, effort, permission_mode, plan_mode, add_dirs, extra_args, env,
    created_at, updated_at
FROM agent_profiles
ORDER BY id;

-- name: GetProfile :one
-- GetProfile returns the agent's profile named name, matched exactly, case included, or
-- sql.ErrNoRows when there is none.
SELECT id, agent, name, config_dir, model, effort, permission_mode, plan_mode, add_dirs, extra_args, env,
    created_at, updated_at
FROM agent_profiles
WHERE agent = ? AND name = ?;

-- name: InsertProfile :exec
-- InsertProfile stores a profile, unless the agent has one with its name stored already:
-- that one is left exactly as it is.
INSERT INTO agent_profiles (
    agent, name, config_dir, model, effort, permission_mode, plan_mode, add_dirs, extra_args, env, created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (agent, name) DO NOTHING;

-- name: DeleteProfiles :exec
-- DeleteProfiles removes the profiles with the given ids. An empty ids matches nothing.
DELETE FROM agent_profiles WHERE id IN (sqlc.slice('ids'));
