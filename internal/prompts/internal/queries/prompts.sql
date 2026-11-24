-- name: ListPrompts :many
-- ListPrompts returns every prompt, by nickname ignoring case and then by id.
SELECT id, name, nickname, applies_to, body, effort, plan_mode, worktree, starter_key, created_at, updated_at,
    models, agent_args, purpose, reply_action, uuid
FROM prompts
ORDER BY nickname COLLATE NOCASE, id;

-- name: GetPrompt :one
-- GetPrompt returns the prompt with id, or sql.ErrNoRows when there is none.
SELECT id, name, nickname, applies_to, body, effort, plan_mode, worktree, starter_key, created_at, updated_at,
    models, agent_args, purpose, reply_action, uuid
FROM prompts
WHERE id = ?;

-- name: CountNickname :one
-- CountNickname counts the prompts other than id called nickname. Nicknames compare
-- ignoring case: the column is declared COLLATE NOCASE.
SELECT COUNT(*) FROM prompts WHERE nickname = ? AND id <> ?;

-- name: InsertPrompt :one
-- InsertPrompt stores a new prompt, an agent prompt or a review reply, under its UUID, and
-- returns its id.
INSERT INTO prompts (
    name, nickname, applies_to, body, models, effort, plan_mode, worktree, agent_args, starter_key, created_at,
    updated_at, purpose, reply_action, uuid
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdatePrompt :execrows
-- UpdatePrompt replaces the title, nickname, text, settings, and reply action of the prompt
-- with id and stamps updated_at. Its purpose, starter key, and created_at stay as they are.
-- No row changes when no prompt has id.
UPDATE prompts SET
    name = ?,
    nickname = ?,
    applies_to = ?,
    body = ?,
    models = ?,
    effort = ?,
    plan_mode = ?,
    worktree = ?,
    agent_args = ?,
    reply_action = ?,
    updated_at = ?
WHERE id = ?;

-- name: DeletePrompt :execrows
-- DeletePrompt removes the prompt with id. The launch defaults' foreign keys refuse it
-- while one names the prompt. No row changes when no prompt has id.
DELETE FROM prompts WHERE id = ?;

-- name: CountPromptUUID :one
-- CountPromptUUID counts the prompts with uuid: one means it is taken.
SELECT COUNT(*) FROM prompts WHERE uuid = ?;

-- name: PromptsWithoutUUID :many
-- PromptsWithoutUUID returns the ids of the prompts with no UUID yet, as a build from
-- before UUIDs inserts them.
SELECT id FROM prompts WHERE uuid = '' ORDER BY id;

-- name: SetPromptUUID :exec
-- SetPromptUUID gives the prompt with id a UUID. Nothing else changes, its updated_at
-- included.
UPDATE prompts SET uuid = ? WHERE id = ?;
