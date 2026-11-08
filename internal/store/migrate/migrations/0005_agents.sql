-- 0005_agents: every coding agent is a backend of its own (Claude Code and OpenAI Codex), and
-- the app makes and keeps track of the worktrees sessions run in.

-- Coding-agent launch profiles, for every agent. agent is the agent's ID ('claude' or
-- 'codex'); a name is unique per agent, so each agent has its own 'default' profile.
-- add_dirs and extra_args are JSON arrays of strings; env is a JSON object of string
-- values. Every Claude profile stored so far keeps its id and settings.
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

INSERT INTO agent_profiles (
    id, agent, name, config_dir, model, effort, permission_mode, plan_mode, add_dirs, extra_args, env, created_at,
    updated_at
)
SELECT id, 'claude', name, config_dir, model, effort, permission_mode, plan_mode, add_dirs, extra_args, env,
    created_at, updated_at
FROM claude_profiles;

DROP TABLE claude_profiles;

-- The default profile is chosen per agent: profile.default.<agent>. The one chosen so far
-- was Claude's.
UPDATE prefs SET key = 'profile.default.claude' WHERE key = 'profile.default';

-- Launch defaults name a profile, a model, and extra flags per agent, since each agent has
-- its own: profiles and models are JSON objects of agent to name, and agent_args a JSON
-- object of agent to a JSON array of arguments. An agent with no entry inherits. The
-- effort, plan mode, worktree, and prompts stay shared by every agent. What was set so far
-- was for Claude.
ALTER TABLE launch_defaults
    ADD COLUMN profiles TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(profiles) AND json_type(profiles) = 'object');

ALTER TABLE launch_defaults
    ADD COLUMN models TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(models) AND json_type(models) = 'object');

ALTER TABLE launch_defaults
    ADD COLUMN agent_args TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(agent_args) AND json_type(agent_args) = 'object');

UPDATE launch_defaults SET
    profiles = CASE WHEN profile = '' THEN '{}' ELSE json_object('claude', profile) END,
    models = CASE WHEN model = '' THEN '{}' ELSE json_object('claude', model) END,
    agent_args = CASE WHEN json_array_length(extra_args) = 0 THEN '{}' ELSE json_object('claude', json(extra_args)) END;

ALTER TABLE launch_defaults DROP COLUMN profile;

ALTER TABLE launch_defaults DROP COLUMN model;

ALTER TABLE launch_defaults DROP COLUMN extra_args;

-- A prompt pins a model and extra flags per agent the same way.
ALTER TABLE prompts
    ADD COLUMN models TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(models) AND json_type(models) = 'object');

ALTER TABLE prompts
    ADD COLUMN agent_args TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(agent_args) AND json_type(agent_args) = 'object');

UPDATE prompts SET
    models = CASE WHEN model = '' THEN '{}' ELSE json_object('claude', model) END,
    agent_args = CASE WHEN json_array_length(extra_args) = 0 THEN '{}' ELSE json_object('claude', json(extra_args)) END;

ALTER TABLE prompts DROP COLUMN model;

ALTER TABLE prompts DROP COLUMN extra_args;

-- The worktrees the app made for pull requests and issues, one per item per clone, which
-- sessions of every agent share.
--
-- clone_path is the clone the worktree belongs to, as the launcher resolved it; worktree
-- is the item's worktree name (pr-<n> or issue-<n>), and owner, name, kind, and number the
-- item. path is the worktree's directory, and branch the branch it was made on
-- (worktree-pr-<n> or worktree-issue-<n>). A row recorded before the app made worktrees
-- itself has path '': its worktree is <clone_path>/.claude/worktrees/<worktree>, which
-- Claude Code made. agent is the agent of the latest session, created_at when the
-- worktree was made or first recorded, last_agent_at when the latest session started
-- (NULL before any did), and launches how many sessions started there.
--
-- The worktrees service checks every row against git each time it lists or sweeps: a row
-- whose worktree is gone is removed, and a worktree on an item's branch with no row is
-- recorded.
CREATE TABLE worktree_records (
    id            INTEGER  PRIMARY KEY,
    clone_path    TEXT     NOT NULL CHECK (clone_path <> ''),
    worktree      TEXT     NOT NULL CHECK (worktree <> ''),
    path          TEXT     NOT NULL DEFAULT '',
    branch        TEXT     NOT NULL DEFAULT '',
    owner         TEXT     NOT NULL COLLATE NOCASE CHECK (owner <> ''),
    name          TEXT     NOT NULL COLLATE NOCASE CHECK (name <> ''),
    kind          TEXT     NOT NULL CHECK (kind IN ('pr', 'issue')),
    number        INTEGER  NOT NULL CHECK (number > 0),
    agent         TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    last_agent_at DATETIME,
    launches      INTEGER  NOT NULL DEFAULT 0 CHECK (launches >= 0),
    UNIQUE (clone_path, worktree)
);

INSERT INTO worktree_records (
    id, clone_path, worktree, path, branch, owner, name, kind, number, agent, created_at, last_agent_at, launches
)
SELECT id, clone_path, worktree, '', 'worktree-' || worktree, owner, name, kind, number, 'claude', launched_at,
    launched_at, 1
FROM worktree_launches;

DROP TABLE worktree_launches;
