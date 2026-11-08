-- name: UpsertDetails :exec
-- UpsertDetails stores an item's details, replacing every column of details stored
-- before.
INSERT INTO item_details (
    item_id, labels, projects, comments, additions, deletions, changed_files, commits, head_oid, mergeable,
    checks_state, checks_passed, checks_failed, checks_pending, checks_skipped, review_decision,
    reviewers, reviewed, approvals, changes_requested, fetched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (item_id) DO UPDATE SET
    labels = excluded.labels,
    projects = excluded.projects,
    comments = excluded.comments,
    additions = excluded.additions,
    deletions = excluded.deletions,
    changed_files = excluded.changed_files,
    commits = excluded.commits,
    head_oid = excluded.head_oid,
    mergeable = excluded.mergeable,
    checks_state = excluded.checks_state,
    checks_passed = excluded.checks_passed,
    checks_failed = excluded.checks_failed,
    checks_pending = excluded.checks_pending,
    checks_skipped = excluded.checks_skipped,
    review_decision = excluded.review_decision,
    reviewers = excluded.reviewers,
    reviewed = excluded.reviewed,
    approvals = excluded.approvals,
    changes_requested = excluded.changes_requested,
    fetched_at = excluded.fetched_at;

-- name: GetActivity :one
-- GetActivity returns the activity a snooze compares from the details of the item with
-- item_id, or sql.ErrNoRows when no refresh has read them.
SELECT head_oid, commits, comments, review_decision, reviewed, approvals, changes_requested
FROM item_details
WHERE item_id = ?;
