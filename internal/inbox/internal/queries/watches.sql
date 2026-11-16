-- name: ListWatchBaseline :many
-- ListWatchBaseline returns what is stored of the items of the account with account_id
-- whose node IDs are among node_ids, for the watch pass to tell which changed since: the
-- item's lists, update time, draft flag, and state, whether a routine's search watched it
-- already, and its details when a read has brought them (a column of missing details is
-- NULL). account_id comes before the list, so its parameter keeps its number however long
-- the list is.
SELECT
    i.id, i.node_id, i.reason_review_requested, i.reason_authored, i.reason_assigned, i.item_updated_at,
    i.is_draft, i.state, i.fetched_at,
    CAST(EXISTS (SELECT 1 FROM item_watches w WHERE w.item_id = i.id) AS BOOLEAN) AS watched,
    d.item_id AS details_item_id, d.head_oid, d.checks_state, d.base_ref, d.merge_state, d.mergeable,
    d.fetched_at AS details_fetched_at
FROM items i
LEFT JOIN item_details d ON d.item_id = i.id
WHERE i.account_id = sqlc.arg(account_id) AND i.node_id IN (sqlc.slice(node_ids));

-- name: EnsureRepo :one
-- EnsureRepo records a repository a routine's search returned an item of, from the little
-- the search reads of it, and returns its id. A repository already stored, by owner and
-- name ignoring case, keeps every column but its node ID, which a search that read one
-- rewrites, and whether it is archived; a new one waits for a details read, or the inbox
-- query, to describe it (fetched_at stays NULL).
INSERT INTO repos (owner, name, node_id, is_archived, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (owner, name) DO UPDATE SET
    node_id = CASE WHEN excluded.node_id <> '' THEN excluded.node_id ELSE repos.node_id END,
    is_archived = excluded.is_archived
RETURNING id;

-- name: UpsertWatchedItem :one
-- UpsertWatchedItem stores what a routine's search says about an item, and returns its id.
-- A new item is in no list; one already stored for the same account, repository, kind,
-- and number has what GitHub says rewritten, its state included, and keeps its lists, the
-- times it arrived in them, and the lists the user left, which only the inbox's own
-- searches and reads change.
INSERT INTO items (
    account_id, repo_id, kind, number, node_id, title, url, author_login, author_type, author_association,
    is_cross_repository, is_draft, head_ref, item_created_at, item_updated_at, fetched_at, state, closed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
    state = CASE WHEN excluded.state <> '' THEN excluded.state ELSE items.state END,
    closed_at = CASE WHEN excluded.state <> '' THEN excluded.closed_at ELSE items.closed_at END,
    fetched_at = excluded.fetched_at
RETURNING id;

-- name: SetItemState :exec
-- SetItemState records the state of the item with id, and when it closed, as a read that
-- carries no more of it said. Nothing changes when no item has id.
UPDATE items SET state = ?, closed_at = ? WHERE id = ?;

-- name: SetRefState :exec
-- SetRefState records the state of every stored item of the repository named owner and
-- name, which match ignoring case, with number, and when it closed, as another service's
-- read of the item's state said. Nothing changes when none is stored.
UPDATE items SET state = sqlc.arg(state), closed_at = sqlc.narg(closed_at)
WHERE number = sqlc.arg(number)
  AND repo_id IN (SELECT id FROM repos WHERE owner = sqlc.arg(owner) AND name = sqlc.arg(name));

-- name: UpsertWatch :exec
-- UpsertWatch records that the search with watch_key returned the item with item_id now:
-- the row is fresh again, and follow is as the routines that share the search ask.
INSERT INTO item_watches (watch_key, item_id, follow, seen_at, stale)
VALUES (?, ?, ?, ?, 0)
ON CONFLICT (watch_key, item_id) DO UPDATE SET
    follow = excluded.follow,
    seen_at = excluded.seen_at,
    stale = 0;

-- name: DeleteUnseenWatches :exec
-- DeleteUnseenWatches deletes the rows of the search with watch_key that its answer, read
-- at seen_before, didn't return: the items left the search.
DELETE FROM item_watches WHERE watch_key = ? AND seen_at < ?;

-- name: MarkWatchesStale :exec
-- MarkWatchesStale marks stale the rows of the search with watch_key that weren't seen at
-- seen_before or later: every row of a search that failed, or a truncated search's tail.
-- They are kept, so their items don't look as if they left the watch.
UPDATE item_watches SET stale = 1 WHERE watch_key = ? AND seen_at < ?;

-- name: DeleteStaleWatches :exec
-- DeleteStaleWatches deletes the stale rows of the search with watch_key last seen before
-- seen_before: a truncated search's tail that has stayed out of its page that long.
DELETE FROM item_watches WHERE watch_key = ? AND stale = 1 AND seen_at < ?;

-- name: DeleteWatchesNotIn :exec
-- DeleteWatchesNotIn deletes the rows of every search but those with one of keys: the
-- searches no routine has any more. keys always holds the empty key, which no search has, so the
-- list is never empty.
DELETE FROM item_watches WHERE watch_key NOT IN (sqlc.slice(keys));

-- name: DeleteItemWatches :exec
-- DeleteItemWatches deletes every watch row of the item with item_id, which a read found
-- merged, closed, or gone: no open search returns it any more.
DELETE FROM item_watches WHERE item_id = ?;

-- name: ListWatchedNodes :many
-- ListWatchedNodes returns the node IDs of every item of the account with account_id a
-- routine's search watches, each once.
SELECT DISTINCT i.node_id
FROM item_watches w
JOIN items i ON i.id = w.item_id
WHERE i.account_id = ? AND i.node_id <> '';

-- name: UpsertChecks :exec
-- UpsertChecks stores a pull request's checks on one head commit, replacing what was
-- stored before.
INSERT INTO item_checks (item_id, head_oid, checks, complete, settled_at, fetched_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (item_id) DO UPDATE SET
    head_oid = excluded.head_oid,
    checks = excluded.checks,
    complete = excluded.complete,
    settled_at = excluded.settled_at,
    fetched_at = excluded.fetched_at;

-- name: ListFactsRows :many
-- ListFactsRows returns what a routine's facts are built from for each stored item among
-- ids, by id: the item with its lists, the times it arrived in them, and its state; its
-- repository; its triage; and its details, file check, and checks when they were read (a
-- column of a missing row is NULL).
SELECT
    i.id, i.kind, i.number, i.title, i.url, i.author_login, i.author_type, i.author_association, i.is_draft,
    i.head_ref, i.is_cross_repository, i.item_created_at, i.item_updated_at, i.reason_review_requested,
    i.reason_authored, i.reason_assigned, i.review_requested_at, i.assigned_at, i.state,
    r.owner AS repo_owner, r.name AS repo_name, r.fetched_at AS repo_fetched_at, r.visibility, r.viewer_permission,
    r.is_fork, r.allow_update_branch, r.language, r.local_path,
    ls.snoozed_until, ls.dismissed_at, ls.pinned_at,
    d.item_id AS details_item_id, d.head_oid, d.base_ref, d.mergeable, d.merge_state, d.review_decision,
    d.checks_state, d.checks_passed, d.checks_failed, d.checks_pending, d.checks_awaiting, d.additions,
    d.deletions, d.changed_files, d.commits, d.comments, d.labels, d.approvals, d.changes_requested,
    d.review_users, d.auto_merge_method,
    f.item_id AS files_item_id, f.head_oid AS files_head_oid, f.files, f.complete AS files_complete,
    f.error AS files_error,
    c.item_id AS checks_item_id, c.head_oid AS checks_head_oid, c.checks, c.complete AS checks_complete
FROM items i
JOIN repos r ON r.id = i.repo_id
LEFT JOIN item_local_state ls ON ls.item_id = i.id
LEFT JOIN item_details d ON d.item_id = i.id
LEFT JOIN item_files f ON f.item_id = i.id
LEFT JOIN item_checks c ON c.item_id = i.id
WHERE i.id IN (sqlc.slice(ids))
ORDER BY i.id;

-- name: ListNodeIDs :many
-- ListNodeIDs returns the id, node ID, and kind of each stored item among ids, by id.
SELECT id, node_id, kind FROM items WHERE id IN (sqlc.slice(ids)) ORDER BY id;

-- name: ListAuthors :many
-- ListAuthors returns each author of the stored items, ignoring case, with their type and
-- how many items they opened, the most first: what a routine's builder suggests.
SELECT author_login, CAST(MAX(author_type) AS TEXT) AS author_type, COUNT(*) AS items
FROM items
WHERE author_login <> ''
GROUP BY author_login COLLATE NOCASE
ORDER BY items DESC, author_login COLLATE NOCASE
LIMIT 200;

-- name: ListOwners :many
-- ListOwners returns each owner of the stored repositories, ignoring case, with how many
-- items of theirs are stored, the most first: what a routine's builder suggests, and the
-- owners a search can name without GitHub answering it with nothing.
SELECT r.owner, COUNT(i.id) AS items
FROM repos r
LEFT JOIN items i ON i.repo_id = r.id
GROUP BY r.owner COLLATE NOCASE
ORDER BY items DESC, r.owner COLLATE NOCASE;

-- name: ListLabelColumns :many
-- ListLabelColumns returns every stored item's labels, as their JSON column, for the
-- builder's suggestions.
SELECT labels FROM item_details WHERE labels <> '[]';

-- name: ListCheckColumns :many
-- ListCheckColumns returns every stored pull request's checks, as their JSON column, for
-- the builder's suggestions.
SELECT checks FROM item_checks WHERE checks <> '[]';

-- name: MarkSearchStale :exec
-- MarkSearchStale marks stale every row of the search with watch_key, which failed: they
-- are kept, so their items don't look as if they left the watch.
UPDATE item_watches SET stale = 1 WHERE watch_key = ?;
