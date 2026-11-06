-- 0010_worktree_cleanups: the worktrees the app cleaned up, newest kept.
--
-- Cleaning up removes a finished worktree, and its own branch, once its pull request merged
-- or its issue closed: by hand from the Worktrees view, or on its own when automatic
-- cleanup is on. Each one is recorded here so the view can show what was removed and when,
-- across restarts, since a toast is easy to miss. automatic tells the sweep from the user.
-- display_path is the worktree's path with ~ for home, as the view shows it; head is the
-- commit it had checked out, which was on GitHub. Only the newest rows are kept.
CREATE TABLE worktree_cleanups (
    id             INTEGER  PRIMARY KEY,
    cleaned_at     DATETIME NOT NULL,
    automatic      INTEGER  NOT NULL DEFAULT 0 CHECK (automatic IN (0, 1)),
    repo           TEXT     NOT NULL CHECK (repo <> ''),
    worktree       TEXT     NOT NULL CHECK (worktree <> ''),
    display_path   TEXT     NOT NULL DEFAULT '',
    deleted_branch TEXT     NOT NULL DEFAULT '',
    head           TEXT     NOT NULL DEFAULT '',
    item_kind      TEXT     NOT NULL DEFAULT '' CHECK (item_kind IN ('', 'pr', 'issue')),
    item_owner     TEXT     NOT NULL DEFAULT '',
    item_name      TEXT     NOT NULL DEFAULT '',
    item_number    INTEGER  NOT NULL DEFAULT 0 CHECK (item_number >= 0),
    item_title     TEXT     NOT NULL DEFAULT '',
    item_url       TEXT     NOT NULL DEFAULT '',
    item_state     TEXT     NOT NULL DEFAULT '' CHECK (item_state IN ('', 'open', 'merged', 'closed'))
);

CREATE INDEX worktree_cleanups_cleaned_at ON worktree_cleanups (cleaned_at);
