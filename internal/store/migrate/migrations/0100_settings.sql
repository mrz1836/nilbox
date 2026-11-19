-- 0100_settings: the conventions every migration follows, and what the user sets up.
--
-- The base migrations, 0100_settings to 0103_autopilot, build whole the schema the
-- migrations v0.5.0 shipped built, 0001_init to 0024_checks_held, which a squash combined
-- into them. A database that ran those is carried over to these without running them; see
-- internal/store/migrate/squashed, which keeps their history and the schema they built.
-- Each file holds the tables of a few owners:
--
--   0100_settings         prefs, profiles, prompts, launch defaults, scripts, and the
--                         Autopilot routines: what the user sets up
--   0101_inbox            the inbox, the user's GitHub notifications, and the app's own
--   0102_runs_and_caches  worktrees, agent sessions, the sheets' caches, avatars, tool
--                         installs, and script runs
--   0103_autopilot        what Autopilot's routines did
--
-- Conventions:
--   - Timestamps are DATETIME columns so the driver reads them back as times. The
--     application writes them in UTC, which keeps their text form sortable.
--   - Booleans are BOOLEAN columns, kept to 0 or 1 by a CHECK, so code generated from
--     this schema reads them as bools. A NULL in a boolean that allows one means
--     "inherit".
--   - GitHub logins and repository names are case-insensitive, so those columns use
--     NOCASE collation.
--   - Credentials are never stored here; the app borrows them at run time.
--
-- The tables are grouped by the one package that owns them (tableOwners in
-- internal/repoguard/datalayer_test.go), the only package whose queries read or write
-- them, and a table comes before the tables that reference it.

-- == prefs ===============================================================================

-- User preferences as key/value pairs.
CREATE TABLE prefs (
    key        TEXT     NOT NULL PRIMARY KEY,
    value      TEXT     NOT NULL,
    updated_at DATETIME NOT NULL
);

-- == profiles ============================================================================

-- Coding-agent launch profiles, for every agent. agent is the agent's ID ('claude' or
-- 'codex'); a name is unique per agent, so each agent has its own 'default' profile.
-- add_dirs and extra_args are JSON arrays of strings; env is a JSON object of string
-- values.
CREATE TABLE agent_profiles (
    id              INTEGER  PRIMARY KEY,
    agent           TEXT     NOT NULL CHECK (agent <> ''),
    name            TEXT     NOT NULL,
    config_dir      TEXT     NOT NULL DEFAULT '',
    model           TEXT     NOT NULL DEFAULT '',
    effort          TEXT     NOT NULL DEFAULT '',
    permission_mode TEXT     NOT NULL DEFAULT '',
    plan_mode       BOOLEAN  NOT NULL DEFAULT 0 CHECK (plan_mode IN (0, 1)),
    add_dirs        TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(add_dirs) AND json_type(add_dirs) = 'array'),
    extra_args      TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(extra_args) AND json_type(extra_args) = 'array'),
    env             TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(env) AND json_type(env) = 'object'),
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    UNIQUE (agent, name)
);

-- == prompts =============================================================================

-- The prompt library: agent prompts and review replies.
--
-- purpose is 'agent' for a prompt a launch fills in for a coding agent: a Go text/template
-- filled in for one pull request or issue. It is 'reply' for a review reply: text the
-- review sheet fills in as the body of a review, which has no launch settings.
-- reply_action names the review a reply is written for ('approve', 'request_changes', or
-- 'comment'), and is '' for an agent prompt.
--
-- name is the prompt's full title and nickname the short label the menus show; agent
-- prompts and replies share one nickname space, and nicknames are unique, ignoring case.
-- applies_to is 'pr', 'issue', or 'any'. starter_key names the starter a row was seeded
-- from ('' for a prompt the user wrote), so the app can restore the original text; the app
-- seeds each starter once, so a deleted starter stays deleted.
--
-- The rest are an agent prompt's launch settings, used whenever the prompt is chosen:
-- effort is '' to inherit, and plan_mode and worktree are NULL to inherit. models pins a
-- model per agent, a JSON object of agent to model, and agent_args extra flags per agent,
-- a JSON object of agent to a JSON array of arguments added after the profile's and the
-- launch defaults'. An agent with no entry inherits.
CREATE TABLE prompts (
    id           INTEGER  PRIMARY KEY,
    name         TEXT     NOT NULL,
    nickname     TEXT     NOT NULL COLLATE NOCASE UNIQUE,
    applies_to   TEXT     NOT NULL CHECK (applies_to IN ('pr', 'issue', 'any')),
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
            AND (reply_action = '') = (purpose = 'agent'))
);

-- == launchdefaults ======================================================================

-- Launch defaults for an org or a repository.
--
-- A row with name '' holds an owner's defaults, which every repository of that owner
-- inherits; a row with a name holds one repository's, which win over its owner's. Rows are
-- keyed by name rather than by repos.id, so an org's defaults need no repository row and
-- outlive the inbox cache.
--
-- Every setting inherits when it is '', NULL, or has no entry: agent is the coding tool
-- ('' is the app's, Claude), pr_prompt_id and issue_prompt_id the default prompt for pull
-- requests and for issues, effort the agent flag, and plan_mode and worktree on or off,
-- each shared by every agent. Each agent has its own profiles, models, and flags, so
-- profiles and models are JSON objects of agent to name, and agent_args a JSON object of
-- agent to a JSON array of arguments added after the profile's (an org's first, then the
-- repository's).
--
-- autopilot_outside is whether Autopilot may run agents on outside contributors' items:
-- NULL to inherit (from the repository's org, else off: there is no app-wide switch),
-- 'off', or 'on'. On still means plan mode: an automatic run never lifts the outside
-- contributor restriction, only a person approving one item does.
--
-- The inbox's cards offer an agent for the situation a pull request is in, and each
-- situation has a default prompt of its own, set and inherited like pr_prompt_id (the
-- repository's, else its org's, else the app's, else the pull request's own chain):
-- checks_prompt_id when its checks fail, conflicts_prompt_id when it conflicts with its
-- base, changes_prompt_id when a reviewer asked for changes, and rereview_prompt_id when
-- the user is asked to review it again.
--
-- A prompt that is a default cannot be deleted: the foreign keys restrict it.
CREATE TABLE launch_defaults (
    id                  INTEGER  PRIMARY KEY,
    owner               TEXT     NOT NULL COLLATE NOCASE,
    name                TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    agent               TEXT     NOT NULL DEFAULT '',
    pr_prompt_id        INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    issue_prompt_id     INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    effort              TEXT     NOT NULL DEFAULT '',
    plan_mode           BOOLEAN  CHECK (plan_mode IN (0, 1)),
    worktree            BOOLEAN  CHECK (worktree IN (0, 1)),
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL,
    profiles            TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(profiles) AND json_type(profiles) = 'object'),
    models              TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(models) AND json_type(models) = 'object'),
    agent_args          TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(agent_args) AND json_type(agent_args) = 'object'),
    autopilot_outside   TEXT     CHECK (autopilot_outside IN ('off', 'on')),
    checks_prompt_id    INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    conflicts_prompt_id INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    changes_prompt_id   INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    rereview_prompt_id  INTEGER  REFERENCES prompts (id) ON DELETE RESTRICT,
    UNIQUE (owner, name)
);

CREATE INDEX launch_defaults_pr_prompt_id ON launch_defaults (pr_prompt_id);
CREATE INDEX launch_defaults_issue_prompt_id ON launch_defaults (issue_prompt_id);
CREATE INDEX launch_defaults_checks_prompt_id ON launch_defaults (checks_prompt_id);
CREATE INDEX launch_defaults_conflicts_prompt_id ON launch_defaults (conflicts_prompt_id);
CREATE INDEX launch_defaults_changes_prompt_id ON launch_defaults (changes_prompt_id);
CREATE INDEX launch_defaults_rereview_prompt_id ON launch_defaults (rereview_prompt_id);

-- == scripts =============================================================================

-- One repository command the user trusted: the sha256 of its effective form when they
-- did, and that form's display text, to show what changed. source is where the command
-- comes from (t3, tasks, or magex) and name its name there.
CREATE TABLE script_trust (
    repo_owner TEXT     NOT NULL COLLATE NOCASE CHECK (repo_owner <> ''),
    repo_name  TEXT     NOT NULL COLLATE NOCASE CHECK (repo_name <> ''),
    source     TEXT     NOT NULL CHECK (source <> ''),
    name       TEXT     NOT NULL CHECK (name <> ''),
    sha256     TEXT     NOT NULL CHECK (length(sha256) = 64),
    command    TEXT     NOT NULL DEFAULT '',
    granted_at DATETIME NOT NULL,
    PRIMARY KEY (repo_owner, repo_name, source, name)
);

-- The user's own scripts, one row each. scope is all, go, or repos (scope_repos, a JSON
-- list of owner/name); runs_in is '' for the app setting, app, or terminal. Go checks scope
-- and runs_in, so later values need no rebuild.
CREATE TABLE user_scripts (
    id          INTEGER  PRIMARY KEY,
    name        TEXT     NOT NULL CHECK (name <> ''),
    command     TEXT     NOT NULL CHECK (command <> ''),
    icon        TEXT     NOT NULL DEFAULT '',
    scope       TEXT     NOT NULL DEFAULT 'all',
    scope_repos TEXT     NOT NULL DEFAULT '[]',
    runs_in     TEXT     NOT NULL DEFAULT '',
    confirm     INTEGER  NOT NULL DEFAULT 0,
    position    INTEGER  NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);

-- == autopilot ===========================================================================

-- One row per routine the user made. spec is the routine's definition, one JSON document
-- with its version (see internal/autopilot/spec), and spec_version that version, as the
-- build that wrote it read it; the name, the description, whether it is on, its mode, and
-- the account it belongs to are its row's, so switching a routine on never touches its
-- definition.
--
-- account_login is the GitHub account that made the routine, '' for one made before any
-- account signed in or brought back from a copy or a backup, which belongs to the account
-- signed in. A routine of another account doesn't run. mode is dryRun, ask, or auto,
-- checked in Go, so a later mode needs no rebuild; a mode this build doesn't know reads
-- as dryRun. template_key names the template a routine was made from, '' for none.
-- sort_order is the routine's place in the list, 0, 1, 2, and so on. enabled_at is when
-- it was last switched on, NULL while it is off. last_error is the last error the routine
-- met, in the window's words, '' for none. broken_reason says why the spec didn't load
-- when the row was last written (newer, unreadable, or invalid, as a routine brought back
-- as it was), '' when it loaded; a read works it out again from the spec, since a newer
-- build may have written it.
--
-- A routine in Ask first whose last five approved runs finished cleanly offers to go on
-- Autopilot; nothing ever changes its mode by itself. graduate_declined_at is when the user
-- last said Not now, NULL while they never did: only approvals after it count toward the
-- next offer.
--
-- A routine is deleted softly: deleted_at is set, and the row stays, so its history keeps
-- its name and its sentence. Names are unique, ignoring case, among the routines not
-- deleted: name_key is the name folded in Go (Unicode's simple case folding, as
-- strings.EqualFold compares), which every comparison of names uses, since NOCASE folds
-- ASCII alone.
CREATE TABLE autopilot_routines (
    id                   INTEGER  PRIMARY KEY,
    account_login        TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    name                 TEXT     NOT NULL CHECK (name <> ''),
    name_key             TEXT     NOT NULL CHECK (name_key <> ''),
    description          TEXT     NOT NULL DEFAULT '',
    enabled              BOOLEAN  NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    mode                 TEXT     NOT NULL CHECK (mode <> ''),
    spec                 TEXT     NOT NULL CHECK (spec <> ''),
    spec_version         INTEGER  NOT NULL DEFAULT 0 CHECK (spec_version >= 0),
    template_key         TEXT     NOT NULL DEFAULT '',
    sort_order           INTEGER  NOT NULL DEFAULT 0,
    enabled_at           DATETIME,
    last_error           TEXT     NOT NULL DEFAULT '',
    broken_reason        TEXT     NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL,
    updated_at           DATETIME NOT NULL,
    deleted_at           DATETIME,
    graduate_declined_at DATETIME,
    CHECK (enabled = 0 OR enabled_at IS NOT NULL),
    CHECK (deleted_at IS NULL OR enabled = 0)
);

CREATE UNIQUE INDEX autopilot_routines_name_key ON autopilot_routines (name_key) WHERE deleted_at IS NULL;

CREATE INDEX autopilot_routines_order ON autopilot_routines (sort_order, id) WHERE deleted_at IS NULL;
