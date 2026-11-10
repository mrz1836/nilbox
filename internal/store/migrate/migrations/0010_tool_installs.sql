-- 0010_tool_installs: the tools the app installed or updated from their GitHub releases.

-- One install or update. A copy counts as installed by the app while its path and sha256
-- match the newest row for that path. archive is the release asset it came from, and
-- replaced the version it replaced, or ''.
CREATE TABLE tool_installs (
    id           INTEGER  PRIMARY KEY,
    tool         TEXT     NOT NULL CHECK (tool <> ''),
    version      TEXT     NOT NULL CHECK (version <> ''),
    path         TEXT     NOT NULL CHECK (path <> ''),
    sha256       TEXT     NOT NULL CHECK (length(sha256) = 64),
    archive      TEXT     NOT NULL DEFAULT '',
    replaced     TEXT     NOT NULL DEFAULT '',
    installed_at DATETIME NOT NULL
);

CREATE INDEX tool_installs_path ON tool_installs (path, installed_at);
