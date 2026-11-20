-- 0109_worktree_copies: the pull request a worktree's copied commits went into, so a squash
-- merge and the deletion of the branch they were copied onto don't undo it.
--
-- The conventions are 0100_settings's. The worklog package owns the table.

-- == worklog =============================================================================

-- A worktree each of whose commits not on GitHub had an identical copy on one pushed branch,
-- as a cherry-pick or a rebase makes, whose pull request was known: one row per worktree.
-- clone_path is its clone and worktree_path the worktree's directory, both resolved; head is
-- the commit it had checked out. ref is the remote-tracking branch the copies were on (as in
-- refs/remotes/origin/feature-x), and repo_owner, repo_name, and pr_number the pull request
-- that branch heads. copy_tip is the branch's commit when last seen holding a copy of every
-- one: it moves on with the branch while the branch still does, so it is most likely the
-- pull request's last head. commits are the worktree's commits not on GitHub then, a JSON
-- array of commit IDs, those whose copies copy_tip holds. created_at is when the row was
-- first written for head, and updated_at when last.
--
-- The worktrees service writes a row when a listing finds a worktree so copied, takes it
-- only while the worktree's HEAD is still head, and deletes it once the worktree is gone or
-- its HEAD moved.
CREATE TABLE worktree_copies (
    id            INTEGER  PRIMARY KEY,
    clone_path    TEXT     NOT NULL CHECK (clone_path <> ''),
    worktree_path TEXT     NOT NULL UNIQUE CHECK (worktree_path <> ''),
    head          TEXT     NOT NULL CHECK (head <> ''),
    ref           TEXT     NOT NULL CHECK (ref <> ''),
    repo_owner    TEXT     NOT NULL COLLATE NOCASE CHECK (repo_owner <> ''),
    repo_name     TEXT     NOT NULL COLLATE NOCASE CHECK (repo_name <> ''),
    pr_number     INTEGER  NOT NULL CHECK (pr_number > 0),
    copy_tip      TEXT     NOT NULL CHECK (copy_tip <> ''),
    commits       TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(commits) AND json_type(commits) = 'array'),
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);
