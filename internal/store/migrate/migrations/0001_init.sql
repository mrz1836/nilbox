-- 0001_init: the initial local schema.
--
-- Conventions:
--   - Timestamps are DATETIME columns so the driver reads them back as times. The
--     application writes them in UTC, which keeps their text form sortable.
--   - Booleans are 0/1 integers.
--   - GitHub logins and repository names are case-insensitive, so those columns use
--     NOCASE collation.
--   - Credentials are never stored here; the app borrows them at run time.

-- The signed-in GitHub account.
CREATE TABLE accounts (
    id         INTEGER  PRIMARY KEY,
    login      TEXT     NOT NULL COLLATE NOCASE UNIQUE,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Repositories seen in the inbox, with per-repo preferences and the matched local clone.
CREATE TABLE repos (
    id         INTEGER  PRIMARY KEY,
    owner      TEXT     NOT NULL COLLATE NOCASE,
    name       TEXT     NOT NULL COLLATE NOCASE,
    muted      INTEGER  NOT NULL DEFAULT 0 CHECK (muted IN (0, 1)),
    pinned     INTEGER  NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)),
    local_path TEXT     NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE (owner, name)
);

-- The unified pull request and issue cache. The reason_* flags record which inbox lists
-- returned the item on the latest refresh; author_association and is_cross_repository
-- feed the outside-contributor trust rule.
CREATE TABLE items (
    id                      INTEGER  PRIMARY KEY,
    account_id              INTEGER  NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    repo_id                 INTEGER  NOT NULL REFERENCES repos (id) ON DELETE CASCADE,
    kind                    TEXT     NOT NULL CHECK (kind IN ('pr', 'issue')),
    number                  INTEGER  NOT NULL CHECK (number > 0),
    node_id                 TEXT     NOT NULL DEFAULT '',
    title                   TEXT     NOT NULL DEFAULT '',
    url                     TEXT     NOT NULL DEFAULT '',
    author_login            TEXT     NOT NULL DEFAULT '',
    author_association      TEXT     NOT NULL DEFAULT '',
    is_cross_repository     INTEGER  NOT NULL DEFAULT 0 CHECK (is_cross_repository IN (0, 1)),
    is_draft                INTEGER  NOT NULL DEFAULT 0 CHECK (is_draft IN (0, 1)),
    head_ref                TEXT     NOT NULL DEFAULT '',
    item_created_at         DATETIME NOT NULL,
    item_updated_at         DATETIME NOT NULL,
    reason_review_requested INTEGER  NOT NULL DEFAULT 0 CHECK (reason_review_requested IN (0, 1)),
    reason_authored         INTEGER  NOT NULL DEFAULT 0 CHECK (reason_authored IN (0, 1)),
    reason_assigned         INTEGER  NOT NULL DEFAULT 0 CHECK (reason_assigned IN (0, 1)),
    fetched_at              DATETIME NOT NULL,
    UNIQUE (account_id, repo_id, kind, number)
);

CREATE INDEX items_repo_id ON items (repo_id);
CREATE INDEX items_account_updated ON items (account_id, item_updated_at);

-- Local-only triage state for an item, such as a done marker.
CREATE TABLE item_local_state (
    item_id    INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    done_at    DATETIME,
    updated_at DATETIME NOT NULL
);

-- User preferences as key/value pairs.
CREATE TABLE prefs (
    key        TEXT     NOT NULL PRIMARY KEY,
    value      TEXT     NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Coding-agent launch profiles. add_dirs and extra_args are JSON arrays of strings; env is
-- a JSON object of string values.
CREATE TABLE claude_profiles (
    id              INTEGER  PRIMARY KEY,
    name            TEXT     NOT NULL UNIQUE,
    config_dir      TEXT     NOT NULL DEFAULT '',
    model           TEXT     NOT NULL DEFAULT '',
    effort          TEXT     NOT NULL DEFAULT '',
    permission_mode TEXT     NOT NULL DEFAULT '',
    plan_mode       INTEGER  NOT NULL DEFAULT 0 CHECK (plan_mode IN (0, 1)),
    add_dirs        TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(add_dirs) AND json_type(add_dirs) = 'array'),
    extra_args      TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(extra_args) AND json_type(extra_args) = 'array'),
    env             TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(env) AND json_type(env) = 'object'),
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);

-- GitHub API rate-limit readings, one row per resource per refresh.
CREATE TABLE rate_snapshots (
    id          INTEGER  PRIMARY KEY,
    account_id  INTEGER  NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    resource    TEXT     NOT NULL,
    limit_total INTEGER  NOT NULL,
    remaining   INTEGER  NOT NULL,
    used        INTEGER  NOT NULL,
    cost        INTEGER  NOT NULL DEFAULT 0,
    reset_at    DATETIME NOT NULL,
    taken_at    DATETIME NOT NULL
);

CREATE INDEX rate_snapshots_resource_taken_at ON rate_snapshots (resource, taken_at);
CREATE INDEX rate_snapshots_account_id ON rate_snapshots (account_id);
