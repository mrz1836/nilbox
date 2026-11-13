-- name: UpsertThread :exec
-- UpsertThread keeps a thread as a pass listed it, at seen_at. The checks' columns
-- (subject_state, subject_closed_at, state_checked_at, and after_moment with its time) keep
-- what they held: only the checks write them.
INSERT INTO gh_threads (
    account_login, thread_id, reason, unread, updated_at, last_read_at, subject_type, subject_title,
    repo_owner, repo_name, item_kind, number, latest_comment_url, seen_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (account_login, thread_id) DO UPDATE SET
    reason = excluded.reason,
    unread = excluded.unread,
    updated_at = excluded.updated_at,
    last_read_at = excluded.last_read_at,
    subject_type = excluded.subject_type,
    subject_title = excluded.subject_title,
    repo_owner = excluded.repo_owner,
    repo_name = excluded.repo_name,
    item_kind = excluded.item_kind,
    number = excluded.number,
    latest_comment_url = excluded.latest_comment_url,
    seen_at = excluded.seen_at;

-- name: DeleteUnseenThreads :execrows
-- DeleteUnseenThreads drops the account's threads a walk of every page didn't list: done on
-- GitHub, or past the newest it reads.
DELETE FROM gh_threads
WHERE account_login = sqlc.arg(account_login) AND thread_id NOT IN (sqlc.slice(seen));

-- name: DeleteUnseenThreadsSince :execrows
-- DeleteUnseenThreadsSince drops the account's threads updated strictly after after that a
-- listing reading every page down to after didn't list: done on GitHub.
DELETE FROM gh_threads
WHERE account_login = sqlc.arg(account_login) AND updated_at > sqlc.arg(after)
    AND thread_id NOT IN (sqlc.slice(seen));

-- name: DeleteAccountThreads :execrows
-- DeleteAccountThreads drops every thread of the account, as a walk of every page that
-- listed none does.
DELETE FROM gh_threads WHERE account_login = ?;

-- name: PruneOtherAccounts :execrows
-- PruneOtherAccounts drops the threads of every other account than the one given that no
-- pass has listed since before.
DELETE FROM gh_threads
WHERE account_login <> sqlc.arg(account_login) AND seen_at < sqlc.arg(before);

-- name: ListItemThreads :many
-- ListItemThreads returns the account's threads about a pull request or an issue, with what
-- the state check last read of each, for the check to pick the ones due.
SELECT thread_id, repo_owner, repo_name, item_kind, number, updated_at, subject_state, state_checked_at
FROM gh_threads
WHERE account_login = ? AND item_kind <> ''
ORDER BY updated_at DESC, thread_id DESC;

-- name: SetSubjectState :exec
-- SetSubjectState records the state GitHub gave for one pull request or issue on every
-- thread of the account's about it, checked at checked_at.
UPDATE gh_threads
SET subject_state = sqlc.arg(subject_state),
    subject_closed_at = sqlc.narg(subject_closed_at),
    state_checked_at = sqlc.arg(state_checked_at)
WHERE account_login = sqlc.arg(account_login) AND repo_owner = sqlc.arg(repo_owner)
    AND repo_name = sqlc.arg(repo_name) AND number = sqlc.arg(number) AND item_kind <> '';

-- name: StampStateChecked :exec
-- StampStateChecked records that GitHub found no such pull request or issue at
-- state_checked_at, on every thread of the account's about it, keeping the state it held.
UPDATE gh_threads
SET state_checked_at = sqlc.arg(state_checked_at)
WHERE account_login = sqlc.arg(account_login) AND repo_owner = sqlc.arg(repo_owner)
    AND repo_name = sqlc.arg(repo_name) AND number = sqlc.arg(number) AND item_kind <> '';

-- name: ListThreads :many
-- ListThreads returns what the window shows of every thread of the account's, the most
-- recently updated first.
SELECT thread_id, reason, unread, updated_at, subject_type, subject_title, repo_owner, repo_name, item_kind, number
FROM gh_threads
WHERE account_login = ?
ORDER BY updated_at DESC, thread_id DESC;
