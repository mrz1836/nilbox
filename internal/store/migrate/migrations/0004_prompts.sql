-- 0004_prompts: the prompt library.
--
-- A prompt is a Go text/template that a launch fills in for one pull request or issue.
-- name is its full title and nickname the short label the launch menu shows; nicknames
-- are unique, ignoring case. applies_to is 'pr', 'issue', or 'any'. starter_key names the
-- starter a row was seeded from ('' for a prompt the user wrote), so the app can restore
-- the original text; the app seeds each starter once, so a deleted starter stays deleted.
--
-- The rest are the prompt's launch settings, used whenever the prompt is chosen: model and
-- effort are '' to inherit, plan_mode and worktree are NULL to inherit, and extra_args is a
-- JSON array of arguments added after the profile's and the launch defaults'.
CREATE TABLE prompts (
    id          INTEGER  PRIMARY KEY,
    name        TEXT     NOT NULL,
    nickname    TEXT     NOT NULL COLLATE NOCASE UNIQUE,
    applies_to  TEXT     NOT NULL CHECK (applies_to IN ('pr', 'issue', 'any')),
    body        TEXT     NOT NULL,
    model       TEXT     NOT NULL DEFAULT '',
    effort      TEXT     NOT NULL DEFAULT '',
    plan_mode   INTEGER  CHECK (plan_mode IN (0, 1)),
    worktree    INTEGER  CHECK (worktree IN (0, 1)),
    extra_args  TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(extra_args) AND json_type(extra_args) = 'array'),
    starter_key TEXT     NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);
