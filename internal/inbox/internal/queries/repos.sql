-- name: UpsertRepo :one
-- UpsertRepo stores what GitHub says about a repository and returns its id. A repository
-- already stored under the same owner and name, which match ignoring case, has every
-- GitHub column rewritten, its owner and name take the casing given, and updated_at is
-- stamped. The local columns (muted, pinned, local_path) and created_at stay as they are.
INSERT INTO repos (
    owner, name, node_id, description, visibility, is_archived, archived_at, is_disabled, is_locked,
    lock_reason, is_fork, parent, default_branch, viewer_permission, viewer_merge_method,
    merge_commit_allowed, squash_merge_allowed, rebase_merge_allowed, auto_merge_allowed,
    delete_branch_on_merge, allow_update_branch, language, language_color, pushed_at, fetched_at,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (owner, name) DO UPDATE SET
    owner = excluded.owner,
    name = excluded.name,
    node_id = excluded.node_id,
    description = excluded.description,
    visibility = excluded.visibility,
    is_archived = excluded.is_archived,
    archived_at = excluded.archived_at,
    is_disabled = excluded.is_disabled,
    is_locked = excluded.is_locked,
    lock_reason = excluded.lock_reason,
    is_fork = excluded.is_fork,
    parent = excluded.parent,
    default_branch = excluded.default_branch,
    viewer_permission = excluded.viewer_permission,
    viewer_merge_method = excluded.viewer_merge_method,
    merge_commit_allowed = excluded.merge_commit_allowed,
    squash_merge_allowed = excluded.squash_merge_allowed,
    rebase_merge_allowed = excluded.rebase_merge_allowed,
    auto_merge_allowed = excluded.auto_merge_allowed,
    delete_branch_on_merge = excluded.delete_branch_on_merge,
    allow_update_branch = excluded.allow_update_branch,
    language = excluded.language,
    language_color = excluded.language_color,
    pushed_at = excluded.pushed_at,
    fetched_at = excluded.fetched_at,
    updated_at = excluded.updated_at
RETURNING id;

-- name: SetRepoLocalPath :exec
-- SetRepoLocalPath remembers the local clone of the repository with id, and stamps
-- updated_at. No row changes when no repository has id.
UPDATE repos SET local_path = ?, updated_at = ? WHERE id = ?;

-- name: ListRepoCounts :many
-- ListRepoCounts returns every repository that is not archived, by owner and then name
-- ignoring case, each with how many of its items are listed now: in at least one of the
-- three lists.
SELECT r.owner, r.name, COUNT(i.id) AS items
FROM repos r
LEFT JOIN items i ON i.repo_id = r.id
  AND (i.reason_review_requested = 1 OR i.reason_authored = 1 OR i.reason_assigned = 1)
WHERE r.is_archived = 0
GROUP BY r.id
ORDER BY r.owner COLLATE NOCASE, r.name COLLATE NOCASE;

-- name: GetRepoDefaultBranch :one
-- GetRepoDefaultBranch returns the default branch a refresh last stored for the repository
-- named owner and name, which match ignoring case.
SELECT default_branch FROM repos WHERE owner = ? AND name = ?;
