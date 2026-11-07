-- name: ListPrompts :many
-- ListPrompts returns every prompt, by nickname ignoring case and then by id.
SELECT id, name, nickname, applies_to, body, model, effort, plan_mode, worktree, extra_args, starter_key,
    created_at, updated_at
FROM prompts
ORDER BY nickname COLLATE NOCASE, id;

-- name: GetPrompt :one
-- GetPrompt returns the prompt with id, or sql.ErrNoRows when there is none.
SELECT id, name, nickname, applies_to, body, model, effort, plan_mode, worktree, extra_args, starter_key,
    created_at, updated_at
FROM prompts
WHERE id = ?;

-- name: CountNickname :one
-- CountNickname counts the prompts other than id called nickname. Nicknames compare
-- ignoring case: the column is declared COLLATE NOCASE.
SELECT COUNT(*) FROM prompts WHERE nickname = ? AND id <> ?;

-- name: InsertPrompt :one
-- InsertPrompt stores a new prompt and returns its id.
INSERT INTO prompts (
    name, nickname, applies_to, body, model, effort, plan_mode, worktree, extra_args, starter_key, created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdatePrompt :execrows
-- UpdatePrompt replaces the title, nickname, text, and settings of the prompt with id and
-- stamps updated_at. Its starter key and created_at stay as they are. No row changes when
-- no prompt has id.
UPDATE prompts SET
    name = ?,
    nickname = ?,
    applies_to = ?,
    body = ?,
    model = ?,
    effort = ?,
    plan_mode = ?,
    worktree = ?,
    extra_args = ?,
    updated_at = ?
WHERE id = ?;

-- name: DeletePrompt :execrows
-- DeletePrompt removes the prompt with id. The launch defaults' foreign keys refuse it
-- while one names the prompt. No row changes when no prompt has id.
DELETE FROM prompts WHERE id = ?;
