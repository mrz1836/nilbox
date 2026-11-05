-- 0003_repo_details: what GitHub says about each repository the inbox sees.
--
-- Every refresh rewrites these from the inbox query, which carries them on each item at no
-- extra cost. fetched_at is NULL until a refresh has described the repository. visibility
-- is PUBLIC, PRIVATE, or INTERNAL; archived_at and pushed_at are NULL when GitHub has none;
-- parent is the "owner/name" a fork was made from; viewer_permission is the viewer's
-- access (ADMIN, MAINTAIN, WRITE, TRIAGE, or READ) and viewer_merge_method the merge
-- method GitHub offers the viewer first; language_color is "#rrggbb" or ''. An archived
-- repository's items are never listed.
ALTER TABLE repos ADD COLUMN node_id TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN visibility TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN is_archived INTEGER NOT NULL DEFAULT 0 CHECK (is_archived IN (0, 1));
ALTER TABLE repos ADD COLUMN archived_at DATETIME;
ALTER TABLE repos ADD COLUMN is_disabled INTEGER NOT NULL DEFAULT 0 CHECK (is_disabled IN (0, 1));
ALTER TABLE repos ADD COLUMN is_locked INTEGER NOT NULL DEFAULT 0 CHECK (is_locked IN (0, 1));
ALTER TABLE repos ADD COLUMN lock_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN is_fork INTEGER NOT NULL DEFAULT 0 CHECK (is_fork IN (0, 1));
ALTER TABLE repos ADD COLUMN parent TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN default_branch TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN viewer_permission TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN viewer_merge_method TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN merge_commit_allowed INTEGER NOT NULL DEFAULT 0 CHECK (merge_commit_allowed IN (0, 1));
ALTER TABLE repos ADD COLUMN squash_merge_allowed INTEGER NOT NULL DEFAULT 0 CHECK (squash_merge_allowed IN (0, 1));
ALTER TABLE repos ADD COLUMN rebase_merge_allowed INTEGER NOT NULL DEFAULT 0 CHECK (rebase_merge_allowed IN (0, 1));
ALTER TABLE repos ADD COLUMN auto_merge_allowed INTEGER NOT NULL DEFAULT 0 CHECK (auto_merge_allowed IN (0, 1));
ALTER TABLE repos ADD COLUMN delete_branch_on_merge INTEGER NOT NULL DEFAULT 0 CHECK (delete_branch_on_merge IN (0, 1));
ALTER TABLE repos ADD COLUMN allow_update_branch INTEGER NOT NULL DEFAULT 0 CHECK (allow_update_branch IN (0, 1));
ALTER TABLE repos ADD COLUMN language TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN language_color TEXT NOT NULL DEFAULT '';
ALTER TABLE repos ADD COLUMN pushed_at DATETIME;
ALTER TABLE repos ADD COLUMN fetched_at DATETIME;
