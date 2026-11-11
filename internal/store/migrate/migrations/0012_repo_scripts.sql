-- 0012_repo_scripts: the user's own scripts, and the repository commands they trusted.

-- One saved script. scope is all, go, or repos (scope_repos, a JSON list of owner/name);
-- runs_in is '' for the app setting, app, or terminal. Go checks scope and runs_in, so
-- later values need no rebuild.
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
