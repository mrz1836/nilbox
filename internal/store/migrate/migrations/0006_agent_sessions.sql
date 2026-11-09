-- 0006_agent_sessions: every launch is an agent session the app follows, linked to its
-- item, from the moment its terminal opens until the agent quits.

-- One coding-agent session started from an item. id is the app's own UUID, minted before
-- the launch; agent_session_id is the agent's own: the Claude Code session, which the
-- launch passes as --session-id, or the Codex thread, found once the agent starts, and ''
-- until then. A session resumed from another names it in resumed_from and shares its
-- agent_session_id. The state is what the agent last reported (see the sessions package),
-- and last_message is the start of what it last said, kept only on this Mac. pid is the
-- agent's process once known, or 0.
CREATE TABLE agent_sessions (
    id               TEXT     PRIMARY KEY CHECK (id <> ''),
    agent_session_id TEXT     NOT NULL DEFAULT '',
    resumed_from     TEXT     NOT NULL DEFAULT '',
    agent            TEXT     NOT NULL CHECK (agent <> ''),
    profile          TEXT     NOT NULL DEFAULT '',
    config_dir       TEXT     NOT NULL DEFAULT '',
    prompt           TEXT     NOT NULL DEFAULT '',
    prompt_head      TEXT     NOT NULL DEFAULT '',
    terminal         TEXT     NOT NULL DEFAULT '',
    item_id          INTEGER  NOT NULL DEFAULT 0,
    owner            TEXT     NOT NULL COLLATE NOCASE CHECK (owner <> ''),
    name             TEXT     NOT NULL COLLATE NOCASE CHECK (name <> ''),
    kind             TEXT     NOT NULL CHECK (kind IN ('pr', 'issue')),
    number           INTEGER  NOT NULL CHECK (number > 0),
    title            TEXT     NOT NULL DEFAULT '',
    url              TEXT     NOT NULL DEFAULT '',
    dir              TEXT     NOT NULL CHECK (dir <> ''),
    clone_path       TEXT     NOT NULL DEFAULT '',
    worktree         TEXT     NOT NULL DEFAULT '',
    pid              INTEGER  NOT NULL DEFAULT 0 CHECK (pid >= 0),
    state            TEXT     NOT NULL CHECK (state <> ''),
    state_detail     TEXT     NOT NULL DEFAULT '',
    last_message     TEXT     NOT NULL DEFAULT '',
    launched_at      DATETIME NOT NULL,
    linked_at        DATETIME,
    state_at         DATETIME NOT NULL,
    ended_at         DATETIME
);

CREATE INDEX agent_sessions_item ON agent_sessions (owner, name, number, launched_at);
CREATE INDEX agent_sessions_state ON agent_sessions (state);
