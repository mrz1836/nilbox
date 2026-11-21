-- 0111_repo_prompts: prompts for repositories, which a launch fills in for a clone rather
-- than for a pull request or an issue.
--
-- The conventions are 0100_settings's. The prompts package owns the prompts table; the
-- launchdefaults package owns launch_defaults, whose prompt columns this migration only
-- puts back as they were.

-- == prompts =============================================================================

-- applies_to may now be 'repo': a prompt a repository's launch fills in, such as an upgrade
-- of its dependencies, which applies to no kind of item. Only an agent prompt applies to
-- repositories; a review reply is the text of a review or a comment on an item, so it never
-- does.
--
-- SQLite can't change a CHECK, so the table is made again with the wider one, its columns
-- in their order, every row copied as it is under its own id, and the old table dropped
-- and the new one renamed, inside this migration's transaction. Foreign keys stay on, and
-- launch_defaults names prompts in six columns, each ON DELETE RESTRICT, which refuses the
-- old table's drop at once, deferred or not. So the launch defaults that name a prompt set
-- their six columns aside in a scratch table and clear them; once the new table holds every
-- prompt under its own id, they take them back, and the scratch table goes. A foreign key
-- names its table by name, so the six name the new table once it is called prompts.

CREATE TABLE launch_defaults_prompt_ids (
    id                  INTEGER  NOT NULL PRIMARY KEY,
    pr_prompt_id        INTEGER,
    issue_prompt_id     INTEGER,
    checks_prompt_id    INTEGER,
    conflicts_prompt_id INTEGER,
    changes_prompt_id   INTEGER,
    rereview_prompt_id  INTEGER
);

INSERT INTO launch_defaults_prompt_ids (
    id, pr_prompt_id, issue_prompt_id, checks_prompt_id, conflicts_prompt_id, changes_prompt_id, rereview_prompt_id
)
SELECT id, pr_prompt_id, issue_prompt_id, checks_prompt_id, conflicts_prompt_id, changes_prompt_id, rereview_prompt_id
FROM launch_defaults
WHERE pr_prompt_id IS NOT NULL OR issue_prompt_id IS NOT NULL OR checks_prompt_id IS NOT NULL
    OR conflicts_prompt_id IS NOT NULL OR changes_prompt_id IS NOT NULL OR rereview_prompt_id IS NOT NULL;

UPDATE launch_defaults
SET pr_prompt_id = NULL, issue_prompt_id = NULL, checks_prompt_id = NULL, conflicts_prompt_id = NULL,
    changes_prompt_id = NULL, rereview_prompt_id = NULL
WHERE id IN (SELECT id FROM launch_defaults_prompt_ids);

CREATE TABLE prompts_next (
    id           INTEGER  PRIMARY KEY,
    name         TEXT     NOT NULL,
    nickname     TEXT     NOT NULL COLLATE NOCASE UNIQUE,
    applies_to   TEXT     NOT NULL CHECK (applies_to IN ('pr', 'issue', 'any', 'repo')),
    body         TEXT     NOT NULL,
    effort       TEXT     NOT NULL DEFAULT '',
    plan_mode    BOOLEAN  CHECK (plan_mode IN (0, 1)),
    worktree     BOOLEAN  CHECK (worktree IN (0, 1)),
    starter_key  TEXT     NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    models       TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(models) AND json_type(models) = 'object'),
    agent_args   TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(agent_args) AND json_type(agent_args) = 'object'),
    purpose      TEXT     NOT NULL DEFAULT 'agent' CHECK (purpose IN ('agent', 'reply')),
    reply_action TEXT     NOT NULL DEFAULT ''
        CHECK (reply_action IN ('', 'approve', 'request_changes', 'comment')
            AND (reply_action = '') = (purpose = 'agent')),
    CHECK (applies_to <> 'repo' OR purpose = 'agent')
);

INSERT INTO prompts_next (
    id, name, nickname, applies_to, body, effort, plan_mode, worktree, starter_key, created_at, updated_at, models,
    agent_args, purpose, reply_action
)
SELECT
    id, name, nickname, applies_to, body, effort, plan_mode, worktree, starter_key, created_at, updated_at, models,
    agent_args, purpose, reply_action
FROM prompts;

DROP TABLE prompts;

ALTER TABLE prompts_next RENAME TO prompts;

UPDATE launch_defaults
SET pr_prompt_id = (SELECT kept.pr_prompt_id FROM launch_defaults_prompt_ids AS kept WHERE kept.id = launch_defaults.id),
    issue_prompt_id = (
        SELECT kept.issue_prompt_id FROM launch_defaults_prompt_ids AS kept WHERE kept.id = launch_defaults.id
    ),
    checks_prompt_id = (
        SELECT kept.checks_prompt_id FROM launch_defaults_prompt_ids AS kept WHERE kept.id = launch_defaults.id
    ),
    conflicts_prompt_id = (
        SELECT kept.conflicts_prompt_id FROM launch_defaults_prompt_ids AS kept WHERE kept.id = launch_defaults.id
    ),
    changes_prompt_id = (
        SELECT kept.changes_prompt_id FROM launch_defaults_prompt_ids AS kept WHERE kept.id = launch_defaults.id
    ),
    rereview_prompt_id = (
        SELECT kept.rereview_prompt_id FROM launch_defaults_prompt_ids AS kept WHERE kept.id = launch_defaults.id
    )
WHERE id IN (SELECT id FROM launch_defaults_prompt_ids);

DROP TABLE launch_defaults_prompt_ids;
