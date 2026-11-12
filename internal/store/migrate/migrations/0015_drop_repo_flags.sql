-- 0015_drop_repo_flags: the repositories' muted and pinned flags, which nothing read or
-- wrote, go. Pinned and muted repositories and orgs are the user's settings, kept in the
-- prefs (repos.pinned and repos.muted), since an org, or a clone the inbox has never seen,
-- has no row here. Each flag's CHECK belongs to its own column, so SQLite drops it with
-- the column.
ALTER TABLE repos DROP COLUMN muted;
ALTER TABLE repos DROP COLUMN pinned;
