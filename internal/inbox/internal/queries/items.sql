-- name: ClearReasons :exec
-- ClearReasons takes every item of the account with account_id out of the three lists.
-- The rows stay, since local state may reference them.
UPDATE items
SET reason_review_requested = 0, reason_authored = 0, reason_assigned = 0
WHERE account_id = ?;

-- name: UpsertItem :one
-- UpsertItem stores what GitHub says about an item, with the lists that returned it, and
-- returns its id. An item already stored for the same account, repository, kind, and
-- number has every column but those four rewritten.
INSERT INTO items (
    account_id, repo_id, kind, number, node_id, title, url, author_login, author_type, author_association,
    is_cross_repository, is_draft, head_ref, item_created_at, item_updated_at,
    reason_review_requested, reason_authored, reason_assigned, fetched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (account_id, repo_id, kind, number) DO UPDATE SET
    node_id = excluded.node_id,
    title = excluded.title,
    url = excluded.url,
    author_login = excluded.author_login,
    author_type = excluded.author_type,
    author_association = excluded.author_association,
    is_cross_repository = excluded.is_cross_repository,
    is_draft = excluded.is_draft,
    head_ref = excluded.head_ref,
    item_created_at = excluded.item_created_at,
    item_updated_at = excluded.item_updated_at,
    reason_review_requested = excluded.reason_review_requested,
    reason_authored = excluded.reason_authored,
    reason_assigned = excluded.reason_assigned,
    fetched_at = excluded.fetched_at
RETURNING id;

-- name: UpdateItem :exec
-- UpdateItem rewrites what GitHub says about the stored item with id, in place. Its
-- account, repository, kind, number, and lists stay as they are.
UPDATE items SET
    node_id = ?,
    title = ?,
    url = ?,
    author_login = ?,
    author_type = ?,
    author_association = ?,
    is_cross_repository = ?,
    is_draft = ?,
    head_ref = ?,
    item_created_at = ?,
    item_updated_at = ?,
    fetched_at = ?
WHERE id = ?;

-- name: UnlistItem :exec
-- UnlistItem takes the item with id out of every list, as a refresh does with an item no
-- search returns, and stamps fetched_at. The row stays, since local state may reference
-- it.
UPDATE items
SET reason_review_requested = 0, reason_authored = 0, reason_assigned = 0, fetched_at = ?
WHERE id = ?;

-- name: GetStoredItem :one
-- GetStoredItem returns the account and node ID of the item with id, or sql.ErrNoRows
-- when there is none. An item without a node ID cannot be asked about on its own, so it
-- reads as not stored.
SELECT account_id, node_id FROM items WHERE id = ? AND node_id <> '';

-- name: ListListedNodeIDs :many
-- ListListedNodeIDs returns the node IDs of the listed items of the account with login,
-- newest activity first.
SELECT i.node_id
FROM items i
JOIN accounts a ON a.id = i.account_id
WHERE a.login = ?
  AND i.node_id <> ''
  AND (i.reason_review_requested = 1 OR i.reason_authored = 1 OR i.reason_assigned = 1)
ORDER BY i.item_updated_at DESC, i.id DESC;

-- name: ListListedFlags :many
-- ListListedFlags returns which items of the account with account_id are in the
-- review-requested and the assigned lists, before a refresh rewrites the flags.
SELECT id, reason_review_requested, reason_assigned FROM items
WHERE account_id = ? AND (reason_review_requested = 1 OR reason_assigned = 1);

-- name: ListItems :many
-- ListItems returns every listed item of the account with account_id, with its
-- repository, its details when a refresh has read them, its last file check, and its
-- triage state: the pinned items first, the most recently pinned first, then the rest by
-- newest activity. A column of a missing details, file check, or triage row is NULL.
SELECT
    i.id, i.kind, i.number, i.title, i.url, r.owner AS repo_owner, r.name AS repo_name,
    i.author_login, i.author_type, i.author_association, i.is_cross_repository, i.is_draft, i.head_ref,
    i.item_updated_at, i.reason_review_requested, i.reason_authored, i.reason_assigned,
    d.item_id AS details_item_id, d.labels, d.projects, d.comments, d.additions, d.deletions,
    d.changed_files, d.commits, d.mergeable, d.checks_state, d.checks_passed, d.checks_failed,
    d.checks_pending, d.checks_skipped, d.review_decision, d.reviewers, d.reviewed, d.approvals,
    d.changes_requested, d.sub_issues_total, d.sub_issues_completed,
    d.fetched_at AS details_fetched_at, d.head_oid,
    f.item_id AS files_item_id, f.head_oid AS files_head_oid, f.total AS files_total, f.files,
    f.complete AS files_complete, f.error AS files_error, f.checked_at AS files_checked_at,
    ls.dismissed_at, ls.snoozed_until, ls.wake_reasons, ls.pinned_at
FROM items i
JOIN repos r ON r.id = i.repo_id
LEFT JOIN item_details d ON d.item_id = i.id
LEFT JOIN item_files f ON f.item_id = i.id
LEFT JOIN item_local_state ls ON ls.item_id = i.id
WHERE i.account_id = ?
  AND (i.reason_review_requested = 1 OR i.reason_authored = 1 OR i.reason_assigned = 1)
ORDER BY ls.pinned_at IS NULL, ls.pinned_at DESC, i.item_updated_at DESC, i.id DESC;

-- name: GetCheckItem :one
-- GetCheckItem returns what checking the files of the item with id needs: the item, its
-- repository, its head commit when a refresh has read its details, and its last file
-- check. A column of missing details or a missing check is NULL. It returns
-- sql.ErrNoRows when no item has id.
SELECT
    i.id, i.kind, i.number, i.url, i.author_login, i.author_type, i.author_association,
    i.is_cross_repository, r.owner AS repo_owner, r.name AS repo_name, d.head_oid,
    f.item_id AS files_item_id, f.head_oid AS files_head_oid, f.total AS files_total, f.files,
    f.complete AS files_complete, f.error AS files_error, f.checked_at AS files_checked_at
FROM items i
JOIN repos r ON r.id = i.repo_id
LEFT JOIN item_details d ON d.item_id = i.id
LEFT JOIN item_files f ON f.item_id = i.id
WHERE i.id = ?;

-- name: ListLaunchItems :many
-- ListLaunchItems returns the stored items among ids, by id, each with its repository,
-- the repository's local clone, and its details when a refresh has read them. A column of
-- missing details is NULL.
SELECT
    i.id, i.kind, i.number, i.url, i.title, i.head_ref, i.author_login, i.author_type, i.author_association,
    i.is_cross_repository, i.is_draft, r.id AS repo_id, r.owner, r.name, r.local_path, r.default_branch,
    d.labels, d.additions, d.deletions, d.changed_files, d.commits, d.mergeable, d.checks_state,
    d.review_decision
FROM items i
JOIN repos r ON r.id = i.repo_id
LEFT JOIN item_details d ON d.item_id = i.id
WHERE i.id IN (sqlc.slice(ids))
ORDER BY i.id;
