-- 0010_worktree_fetched_head: the commit a worktree was made at, fetched from GitHub.
--
-- The conventions are 0001_init's. The worklog package owns every column here.

-- == worktree_records ====================================================================

-- fetched_head is the commit the app made the worktree at, just fetched from GitHub: a pull
-- request's head (refs/pull/<n>/head, which the fetch puts on the item's own branch, not on
-- a remote-tracking branch), or the default branch an issue's worktree starts from; '' for
-- a worktree the app found, or added on a branch left from an earlier one, and for one
-- recorded before this column. Every commit it reaches came from GitHub, so the Worktrees
-- view counts only the commits past it as not on GitHub, and a pull request that moves on
-- after the launch leaves the worktree's own commits counted as none. A later record of the
-- same worktree that knows no such commit keeps the one stored.
ALTER TABLE worktree_records ADD COLUMN fetched_head TEXT NOT NULL DEFAULT '';
