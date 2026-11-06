-- 0005_launch_defaults: launch defaults for an org or a repository.
--
-- A row with name '' holds an owner's defaults, which every repository of that owner
-- inherits; a row with a name holds one repository's, which win over its owner's. Rows are
-- keyed by name rather than by repos.id, so an org's defaults need no repository row and
-- outlive the inbox cache.
--
-- Every setting inherits when it is '' or NULL: agent is the coding tool ('' is the app's,
-- Claude), profile a profile name, pr_prompt_id and issue_prompt_id the default prompt for
-- pull requests and for issues, model and effort the agent flags, plan_mode and worktree
-- on or off. extra_args is a JSON array of arguments added after the profile's (an org's
-- first, then the repository's). A prompt that is a default cannot be deleted.
CREATE TABLE launch_defaults (
    id              INTEGER  PRIMARY KEY,
    owner           TEXT     NOT NULL COLLATE NOCASE,
    name            TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    agent           TEXT     NOT NULL DEFAULT '',
    profile         TEXT     NOT NULL DEFAULT '',
    pr_prompt_id    INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    issue_prompt_id INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    model           TEXT     NOT NULL DEFAULT '',
    effort          TEXT     NOT NULL DEFAULT '',
    plan_mode       INTEGER  CHECK (plan_mode IN (0, 1)),
    worktree        INTEGER  CHECK (worktree IN (0, 1)),
    extra_args      TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(extra_args) AND json_type(extra_args) = 'array'),
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    UNIQUE (owner, name)
);

CREATE INDEX launch_defaults_pr_prompt_id ON launch_defaults (pr_prompt_id);
CREATE INDEX launch_defaults_issue_prompt_id ON launch_defaults (issue_prompt_id);
