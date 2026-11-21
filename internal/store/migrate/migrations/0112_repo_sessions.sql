-- 0112_repo_sessions: sessions launched for a repository rather than for a pull request or
-- an issue, as an upgrade of its dependencies is.
--
-- The conventions are 0100_settings's. The sessions package owns agent_sessions.

-- == agent_sessions ======================================================================

-- A session a repository's launch started has no item: its kind is '' and its number 0,
-- its item_id 0, and its worktree the one its launch made for it, as deps-20261121, never
-- ''. Its title is its prompt's name, and its url the repository's page. A session of a
-- pull request or an issue keeps its kind and a positive number, as before.
--
-- SQLite can't change a CHECK, so the table is made again with the wider ones, its columns
-- in their order and every other constraint and default as 0102_runs_and_caches made them,
-- every row copied as it is, the old table dropped and the new one renamed, and its two
-- indexes made again, inside this migration's transaction. No table references
-- agent_sessions, and no trigger or view names it.

CREATE TABLE agent_sessions_next (
    id                  TEXT     PRIMARY KEY CHECK (id <> ''),
    agent_session_id    TEXT     NOT NULL DEFAULT '',
    resumed_from        TEXT     NOT NULL DEFAULT '',
    agent               TEXT     NOT NULL CHECK (agent <> ''),
    profile             TEXT     NOT NULL DEFAULT '',
    config_dir          TEXT     NOT NULL DEFAULT '',
    prompt              TEXT     NOT NULL DEFAULT '',
    prompt_head         TEXT     NOT NULL DEFAULT '',
    terminal            TEXT     NOT NULL DEFAULT '',
    item_id             INTEGER  NOT NULL DEFAULT 0,
    owner               TEXT     NOT NULL COLLATE NOCASE CHECK (owner <> ''),
    name                TEXT     NOT NULL COLLATE NOCASE CHECK (name <> ''),
    kind                TEXT     NOT NULL CHECK (kind IN ('', 'pr', 'issue')),
    number              INTEGER  NOT NULL CHECK (number >= 0),
    title               TEXT     NOT NULL DEFAULT '',
    url                 TEXT     NOT NULL DEFAULT '',
    dir                 TEXT     NOT NULL CHECK (dir <> ''),
    clone_path          TEXT     NOT NULL DEFAULT '',
    worktree            TEXT     NOT NULL DEFAULT '',
    pid                 INTEGER  NOT NULL DEFAULT 0 CHECK (pid >= 0),
    state               TEXT     NOT NULL CHECK (state <> ''),
    state_detail        TEXT     NOT NULL DEFAULT '',
    last_message        TEXT     NOT NULL DEFAULT '',
    launched_at         DATETIME NOT NULL,
    linked_at           DATETIME,
    state_at            DATETIME NOT NULL,
    ended_at            DATETIME,
    permission          TEXT     NOT NULL DEFAULT '',
    worktree_action     TEXT     NOT NULL DEFAULT '',
    launch_notice       TEXT     NOT NULL DEFAULT '',
    run_id              INTEGER  NOT NULL DEFAULT 0 CHECK (run_id >= 0),
    github_event        TEXT     NOT NULL DEFAULT ''
        CHECK (github_event IN ('', 'approved', 'changes_requested', 'merged', 'closed')),
    github_event_commit TEXT     NOT NULL DEFAULT '',
    github_event_at     DATETIME,
    CHECK ((kind = '') = (number = 0)),
    CHECK (kind <> '' OR (item_id = 0 AND worktree <> ''))
);

INSERT INTO agent_sessions_next (
    id, agent_session_id, resumed_from, agent, profile, config_dir, prompt, prompt_head, terminal, item_id, owner,
    name, kind, number, title, url, dir, clone_path, worktree, pid, state, state_detail, last_message, launched_at,
    linked_at, state_at, ended_at, permission, worktree_action, launch_notice, run_id, github_event,
    github_event_commit, github_event_at
)
SELECT
    id, agent_session_id, resumed_from, agent, profile, config_dir, prompt, prompt_head, terminal, item_id, owner,
    name, kind, number, title, url, dir, clone_path, worktree, pid, state, state_detail, last_message, launched_at,
    linked_at, state_at, ended_at, permission, worktree_action, launch_notice, run_id, github_event,
    github_event_commit, github_event_at
FROM agent_sessions;

DROP TABLE agent_sessions;

ALTER TABLE agent_sessions_next RENAME TO agent_sessions;

CREATE INDEX agent_sessions_item ON agent_sessions (owner, name, number, launched_at);
CREATE INDEX agent_sessions_state ON agent_sessions (state);
