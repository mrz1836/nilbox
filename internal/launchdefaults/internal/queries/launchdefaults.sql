-- name: ListLaunchDefaults :many
-- ListLaunchDefaults returns every org's and repository's launch defaults, by owner and
-- then name ignoring case, so each owner's own defaults (name '') come before its
-- repositories'.
SELECT id, owner, name, agent, profile, pr_prompt_id, issue_prompt_id, model, effort, plan_mode, worktree,
    extra_args, created_at, updated_at
FROM launch_defaults
ORDER BY owner COLLATE NOCASE, name COLLATE NOCASE;

-- name: ListPromptUses :many
-- ListPromptUses returns the owner, name, and default prompts of every launch default
-- that names a prompt, in the order ListLaunchDefaults uses.
SELECT owner, name, pr_prompt_id, issue_prompt_id
FROM launch_defaults
WHERE pr_prompt_id IS NOT NULL OR issue_prompt_id IS NOT NULL
ORDER BY owner COLLATE NOCASE, name COLLATE NOCASE;

-- name: SaveLaunchDefaults :one
-- SaveLaunchDefaults stores an org's or a repository's launch defaults and returns the
-- stored row. Defaults already stored for the same owner and name, which match ignoring
-- case since both columns are declared COLLATE NOCASE, have every setting replaced and
-- updated_at stamped; their owner, name, and created_at stay as first saved.
INSERT INTO launch_defaults (
    owner, name, agent, profile, pr_prompt_id, issue_prompt_id, model, effort, plan_mode, worktree, extra_args,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (owner, name) DO UPDATE SET
    agent = excluded.agent,
    profile = excluded.profile,
    pr_prompt_id = excluded.pr_prompt_id,
    issue_prompt_id = excluded.issue_prompt_id,
    model = excluded.model,
    effort = excluded.effort,
    plan_mode = excluded.plan_mode,
    worktree = excluded.worktree,
    extra_args = excluded.extra_args,
    updated_at = excluded.updated_at
RETURNING id, owner, name, agent, profile, pr_prompt_id, issue_prompt_id, model, effort, plan_mode, worktree,
    extra_args, created_at, updated_at;

-- name: DeleteLaunchDefaults :execrows
-- DeleteLaunchDefaults removes an org's (name '') or a repository's launch defaults,
-- matching the owner and name ignoring case. No row changes when none are stored.
DELETE FROM launch_defaults WHERE owner = ? AND name = ?;
