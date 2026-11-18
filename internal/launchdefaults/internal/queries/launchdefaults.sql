-- name: ListLaunchDefaults :many
-- ListLaunchDefaults returns every org's and repository's launch defaults, by owner and
-- then name ignoring case, so each owner's own defaults (name '') come before its
-- repositories'.
SELECT id, owner, name, agent, pr_prompt_id, issue_prompt_id, effort, plan_mode, worktree, created_at, updated_at,
    profiles, models, agent_args, autopilot_outside, checks_prompt_id, conflicts_prompt_id, changes_prompt_id,
    rereview_prompt_id
FROM launch_defaults
ORDER BY owner COLLATE NOCASE, name COLLATE NOCASE;

-- name: ListPromptUses :many
-- ListPromptUses returns the owner, name, and default prompts of every launch default
-- that names a prompt, in the order ListLaunchDefaults uses: the prompt for each kind of
-- item, and for each situation a pull request can be in.
SELECT owner, name, pr_prompt_id, issue_prompt_id, checks_prompt_id, conflicts_prompt_id, changes_prompt_id,
    rereview_prompt_id
FROM launch_defaults
WHERE pr_prompt_id IS NOT NULL OR issue_prompt_id IS NOT NULL OR checks_prompt_id IS NOT NULL
    OR conflicts_prompt_id IS NOT NULL OR changes_prompt_id IS NOT NULL OR rereview_prompt_id IS NOT NULL
ORDER BY owner COLLATE NOCASE, name COLLATE NOCASE;

-- name: SaveLaunchDefaults :one
-- SaveLaunchDefaults stores an org's or a repository's launch defaults and returns the
-- stored row. Defaults already stored for the same owner and name, which match ignoring
-- case since both columns are declared COLLATE NOCASE, have every setting replaced and
-- updated_at stamped; their owner, name, and created_at stay as first saved.
INSERT INTO launch_defaults (
    owner, name, agent, profiles, pr_prompt_id, issue_prompt_id, checks_prompt_id, conflicts_prompt_id,
    changes_prompt_id, rereview_prompt_id, models, effort, plan_mode, worktree, agent_args, autopilot_outside,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (owner, name) DO UPDATE SET
    agent = excluded.agent,
    profiles = excluded.profiles,
    pr_prompt_id = excluded.pr_prompt_id,
    issue_prompt_id = excluded.issue_prompt_id,
    checks_prompt_id = excluded.checks_prompt_id,
    conflicts_prompt_id = excluded.conflicts_prompt_id,
    changes_prompt_id = excluded.changes_prompt_id,
    rereview_prompt_id = excluded.rereview_prompt_id,
    models = excluded.models,
    effort = excluded.effort,
    plan_mode = excluded.plan_mode,
    worktree = excluded.worktree,
    agent_args = excluded.agent_args,
    autopilot_outside = excluded.autopilot_outside,
    updated_at = excluded.updated_at
RETURNING id, owner, name, agent, pr_prompt_id, issue_prompt_id, effort, plan_mode, worktree, created_at, updated_at,
    profiles, models, agent_args, autopilot_outside, checks_prompt_id, conflicts_prompt_id, changes_prompt_id,
    rereview_prompt_id;

-- name: DeleteLaunchDefaults :execrows
-- DeleteLaunchDefaults removes an org's (name '') or a repository's launch defaults,
-- matching the owner and name ignoring case. No row changes when none are stored.
DELETE FROM launch_defaults WHERE owner = ? AND name = ?;

-- name: DeleteAllLaunchDefaults :exec
-- DeleteAllLaunchDefaults removes every org's and repository's launch defaults, as an
-- import that replaces them does first.
DELETE FROM launch_defaults;
