-- name: SetTriage :execrows
-- SetTriage records the triage state of the item with item_id, replacing any earlier
-- one, and clears the reasons an earlier snooze woke for. A pin stays as it is. No row
-- changes when no item has item_id.
INSERT INTO item_local_state (item_id, dismissed_at, snoozed_until, snooze_baseline, wake_reasons, updated_at)
SELECT id, ?, ?, ?, NULL, ? FROM items WHERE id = ?
ON CONFLICT (item_id) DO UPDATE SET
    dismissed_at = excluded.dismissed_at,
    snoozed_until = excluded.snoozed_until,
    snooze_baseline = excluded.snooze_baseline,
    wake_reasons = NULL,
    updated_at = excluded.updated_at;

-- name: Pin :execrows
-- Pin pins the item with item_id to the top of its lists, keeping its triage state and
-- the time of an earlier pin, so pinning a pinned item does not move it. No row changes
-- when no item has item_id.
INSERT INTO item_local_state (item_id, pinned_at, updated_at)
SELECT id, ?, ? FROM items WHERE id = ?
ON CONFLICT (item_id) DO UPDATE SET
    pinned_at = COALESCE(item_local_state.pinned_at, excluded.pinned_at),
    updated_at = excluded.updated_at;

-- name: Restore :exec
-- Restore ends the dismissal or snooze of the item with item_id, and forgets what woke
-- the snooze, leaving a pin as it is.
UPDATE item_local_state
SET dismissed_at = NULL, snoozed_until = NULL, snooze_baseline = NULL, wake_reasons = NULL, updated_at = ?
WHERE item_id = ?;

-- name: Unpin :exec
-- Unpin unpins the item with item_id, leaving its dismissal or snooze as it is.
UPDATE item_local_state SET pinned_at = NULL, updated_at = ? WHERE item_id = ?;

-- name: PruneState :exec
-- PruneState deletes the local state of the item with item_id once nothing is left in
-- it: not pinned, dismissed, or snoozed, nor back from a snooze.
DELETE FROM item_local_state
WHERE item_id = ? AND pinned_at IS NULL AND dismissed_at IS NULL AND snoozed_until IS NULL;

-- name: ListRunningSnoozes :many
-- ListRunningSnoozes returns the snoozes of the items of the account with account_id
-- that still run after now, on items whose details are known, each with its baseline,
-- the author's type, and the activity the details hold.
SELECT ls.item_id, ls.snooze_baseline, i.author_type, d.head_oid, d.commits, d.comments, d.review_decision,
    d.reviewed, d.approvals, d.changes_requested
FROM item_local_state ls
JOIN items i ON i.id = ls.item_id
JOIN item_details d ON d.item_id = ls.item_id
WHERE i.account_id = ? AND ls.dismissed_at IS NULL AND ls.snoozed_until > ?;

-- name: SetSnoozeBaseline :exec
-- SetSnoozeBaseline records the activity the snooze of the item with item_id compares
-- with.
UPDATE item_local_state SET snooze_baseline = ? WHERE item_id = ?;

-- name: WakeSnooze :exec
-- WakeSnooze ends the snooze of the item with item_id at snoozed_until, with the reasons
-- new activity woke it for.
UPDATE item_local_state SET snoozed_until = ?, wake_reasons = ?, updated_at = ? WHERE item_id = ?;

-- name: ListSetAside :many
-- ListSetAside returns which of the items with ids the user has set aside at now:
-- dismissed, or snoozed past it. now comes before the list, so its parameter keeps its
-- number however long the list is.
SELECT item_id FROM item_local_state
WHERE (dismissed_at IS NOT NULL OR snoozed_until > sqlc.arg(now)) AND item_id IN (sqlc.slice(ids));

-- name: GetTriageItem :one
-- GetTriageItem returns the item with id as an outcome names it (the account whose inbox
-- lists it, its repository, number, and kind) with its dismissal and snooze, each NULL
-- when it has none, or sql.ErrNoRows when no item has id.
SELECT a.login AS account_login, r.owner AS repo_owner, r.name AS repo_name, i.number, i.kind,
    ls.dismissed_at, ls.snoozed_until
FROM items i
JOIN accounts a ON a.id = i.account_id
JOIN repos r ON r.id = i.repo_id
LEFT JOIN item_local_state ls ON ls.item_id = i.id
WHERE i.id = ?;
