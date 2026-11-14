-- name: UpsertMoment :exec
-- UpsertMoment records that the user finished an item in the app, by trigger, at, heard of
-- at created_at: a later moment of the same trigger replaces the one before.
INSERT INTO gh_thread_moments (account_login, repo_owner, repo_name, item_kind, number, trigger, at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (account_login, repo_owner, repo_name, item_kind, number, trigger) DO UPDATE SET
    at = excluded.at,
    created_at = excluded.created_at;

-- name: DeleteMoment :execrows
-- DeleteMoment drops the account's moment of trigger for one item, as a restore after a
-- dismissal does.
DELETE FROM gh_thread_moments
WHERE account_login = sqlc.arg(account_login) AND repo_owner = sqlc.arg(repo_owner)
    AND repo_name = sqlc.arg(repo_name) AND number = sqlc.arg(number) AND trigger = sqlc.arg(trigger);

-- name: ListMoments :many
-- ListMoments returns every moment of the account's.
SELECT repo_owner, repo_name, item_kind, number, trigger, at, created_at
FROM gh_thread_moments
WHERE account_login = ?
ORDER BY created_at, trigger;

-- name: ListItemMoments :many
-- ListItemMoments returns the account's moments of one item, as ListMoments does.
SELECT repo_owner, repo_name, item_kind, number, trigger, at, created_at
FROM gh_thread_moments
WHERE account_login = sqlc.arg(account_login) AND repo_owner = sqlc.arg(repo_owner)
    AND repo_name = sqlc.arg(repo_name) AND number = sqlc.arg(number)
ORDER BY created_at, trigger;

-- name: PruneMoments :execrows
-- PruneMoments drops the moments heard of before before.
DELETE FROM gh_thread_moments WHERE created_at < ?;

-- name: ListDecideThreads :many
-- ListDecideThreads returns the account's threads about a pull request or an issue, with
-- what deciding whether to clear one reads, the most recently updated first.
SELECT thread_id, reason, unread, updated_at, subject_title, repo_owner, repo_name, item_kind, number,
    subject_state, subject_closed_at, state_checked_at, after_moment, after_moment_checked_at, latest_comment_url
FROM gh_threads
WHERE account_login = ? AND item_kind <> ''
ORDER BY updated_at DESC, thread_id DESC;

-- name: GetDecideThread :one
-- GetDecideThread returns one of the account's threads as ListDecideThreads does.
SELECT thread_id, reason, unread, updated_at, subject_title, repo_owner, repo_name, item_kind, number,
    subject_state, subject_closed_at, state_checked_at, after_moment, after_moment_checked_at, latest_comment_url
FROM gh_threads
WHERE account_login = ? AND thread_id = ?;

-- name: SetAfterMoment :exec
-- SetAfterMoment records who acted on a thread after its item was finished, checked at
-- checked_at.
UPDATE gh_threads
SET after_moment = sqlc.arg(after_moment), after_moment_checked_at = sqlc.arg(checked_at)
WHERE account_login = sqlc.arg(account_login) AND thread_id = sqlc.arg(thread_id);

-- name: DeleteThread :exec
-- DeleteThread drops one thread of the account's, marked done on GitHub.
DELETE FROM gh_threads WHERE account_login = ? AND thread_id = ?;

-- name: SetThreadRead :exec
-- SetThreadRead records one thread of the account's as read, marked read on GitHub; who
-- acted after its item was finished no longer matters.
UPDATE gh_threads SET unread = 0, after_moment = '' WHERE account_login = ? AND thread_id = ?;

-- name: InsertClear :one
-- InsertClear queues a clear decided at queued_at, to be sent once send_after has passed.
INSERT INTO gh_thread_clears (
    account_login, thread_id, repo_owner, repo_name, item_kind, number, title, reason, trigger, action, state,
    thread_updated_at, queued_at, send_after
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?)
RETURNING id;

-- name: ListClearMarks :many
-- ListClearMarks returns the account's clears that bear on queueing another: those queued,
-- sent, kept, and failed.
SELECT id, thread_id, trigger, state, thread_updated_at
FROM gh_thread_clears
WHERE account_login = ? AND state IN ('queued', 'sent', 'kept', 'failed')
ORDER BY id;

-- name: GetClear :one
-- GetClear returns one clear.
SELECT id, account_login, thread_id, repo_owner, repo_name, item_kind, number, title, reason, trigger, action, state,
    thread_updated_at, queued_at, send_after, attempts
FROM gh_thread_clears
WHERE id = ?;

-- name: NextDueClear :one
-- NextDueClear returns the account's queued clear due first as of now: the earliest
-- send_after, then the first queued.
SELECT id
FROM gh_thread_clears
WHERE account_login = sqlc.arg(account_login) AND state = 'queued' AND send_after <= sqlc.arg(now)
ORDER BY send_after, id
LIMIT 1;

-- name: NextSendAfter :many
-- NextSendAfter returns when the account's queued clear due first may be sent, or no row.
SELECT send_after
FROM gh_thread_clears
WHERE account_login = ? AND state = 'queued'
ORDER BY send_after, id
LIMIT 1;

-- name: SettleClear :execrows
-- SettleClear takes a queued clear out of the queue as state, settled at settled_at.
UPDATE gh_thread_clears
SET state = sqlc.arg(state), settled_at = sqlc.arg(settled_at), sent_at = sqlc.narg(sent_at),
    error = sqlc.arg(error), skip_reason = sqlc.arg(skip_reason)
WHERE id = sqlc.arg(id) AND state = 'queued';

-- name: FailClear :execrows
-- FailClear takes a queued clear out of the queue as failed, after its last attempt.
UPDATE gh_thread_clears
SET state = 'failed', settled_at = sqlc.arg(settled_at), attempts = sqlc.arg(attempts), error = sqlc.arg(error)
WHERE id = sqlc.arg(id) AND state = 'queued';

-- name: RetryClear :execrows
-- RetryClear records a queued clear's failed send, and when it is tried again.
UPDATE gh_thread_clears
SET attempts = sqlc.arg(attempts), error = sqlc.arg(error), send_after = sqlc.arg(send_after)
WHERE id = sqlc.arg(id) AND state = 'queued';

-- name: SetClearAction :execrows
-- SetClearAction records what a queued clear does as the rules say now.
UPDATE gh_thread_clears SET action = ? WHERE id = ? AND state = 'queued';

-- name: ClearByHand :execrows
-- ClearByHand turns a queued clear into the user's own, sent at send_after.
UPDATE gh_thread_clears
SET trigger = 'by_hand', action = sqlc.arg(action), send_after = sqlc.arg(send_after)
WHERE id = sqlc.arg(id) AND state = 'queued';

-- name: KeepClears :execrows
-- KeepClears marks the queued clears with the IDs kept: the user undid them.
UPDATE gh_thread_clears
SET state = 'kept', settled_at = sqlc.arg(settled_at)
WHERE state = 'queued' AND id IN (sqlc.slice(ids));

-- name: KeepItemClears :execrows
-- KeepItemClears marks the account's queued clears of trigger about one item kept, as a
-- restore after a dismissal does.
UPDATE gh_thread_clears
SET state = 'kept', settled_at = sqlc.arg(settled_at)
WHERE account_login = sqlc.arg(account_login) AND repo_owner = sqlc.arg(repo_owner)
    AND repo_name = sqlc.arg(repo_name) AND number = sqlc.arg(number) AND trigger = sqlc.arg(trigger)
    AND state = 'queued';

-- name: SkipQueuedClears :execrows
-- SkipQueuedClears skips every queued clear, for skip_reason: reading turned off.
UPDATE gh_thread_clears
SET state = 'skipped', settled_at = sqlc.arg(settled_at), skip_reason = sqlc.arg(skip_reason)
WHERE state = 'queued';

-- name: SkipQueuedAutoClears :execrows
-- SkipQueuedAutoClears skips every queued clear the user didn't ask for, for skip_reason:
-- clearing on its own turned off.
UPDATE gh_thread_clears
SET state = 'skipped', settled_at = sqlc.arg(settled_at), skip_reason = sqlc.arg(skip_reason)
WHERE state = 'queued' AND trigger <> 'by_hand';

-- name: SkipOtherAccountsQueued :execrows
-- SkipOtherAccountsQueued skips the queued clears of every account but the one given that
-- were queued before before: the account they wait for hasn't come back.
UPDATE gh_thread_clears
SET state = 'skipped', settled_at = sqlc.arg(settled_at), skip_reason = sqlc.arg(skip_reason)
WHERE state = 'queued' AND account_login <> sqlc.arg(account_login) AND queued_at < sqlc.arg(before);

-- name: ListQueuedClears :many
-- ListQueuedClears returns the account's queued clears, due first first.
SELECT id, thread_id, repo_owner, repo_name, item_kind, number, title, trigger, action, send_after
FROM gh_thread_clears
WHERE account_login = ? AND state = 'queued'
ORDER BY send_after, id;

-- name: ListRecentClears :many
-- ListRecentClears returns the account's clears that settled, the latest first.
SELECT id, thread_id, repo_owner, repo_name, item_kind, number, title, trigger, action, state, settled_at, error,
    skip_reason
FROM gh_thread_clears
WHERE account_login = sqlc.arg(account_login) AND state <> 'queued'
ORDER BY settled_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: PruneClears :execrows
-- PruneClears drops the clears that settled before before.
DELETE FROM gh_thread_clears WHERE state <> 'queued' AND settled_at < ?;
