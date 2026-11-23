-- name: UpsertDetails :exec
-- UpsertDetails stores an item's details, replacing every column of details stored
-- before.
INSERT INTO item_details (
    item_id, labels, projects, comments, additions, deletions, changed_files, commits, head_oid, base_ref, mergeable,
    merge_state,
    checks_state, checks_passed, checks_failed, checks_pending, checks_skipped, checks_awaiting,
    checks_awaiting_on_github, checks_held,
    review_decision, reviewers, reviewed, approvals, changes_requested, sub_issues_total, sub_issues_completed,
    review_users, review_teams, auto_merge_method, auto_merge_at, issue_info, closing_pulls, approving_bots,
    review_texts, fetched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (item_id) DO UPDATE SET
    labels = excluded.labels,
    projects = excluded.projects,
    comments = excluded.comments,
    additions = excluded.additions,
    deletions = excluded.deletions,
    changed_files = excluded.changed_files,
    commits = excluded.commits,
    head_oid = excluded.head_oid,
    base_ref = excluded.base_ref,
    merge_state = excluded.merge_state,
    mergeable = excluded.mergeable,
    checks_state = excluded.checks_state,
    checks_passed = excluded.checks_passed,
    checks_failed = excluded.checks_failed,
    checks_pending = excluded.checks_pending,
    checks_skipped = excluded.checks_skipped,
    checks_awaiting = excluded.checks_awaiting,
    checks_awaiting_on_github = excluded.checks_awaiting_on_github,
    checks_held = excluded.checks_held,
    review_decision = excluded.review_decision,
    reviewers = excluded.reviewers,
    reviewed = excluded.reviewed,
    approvals = excluded.approvals,
    changes_requested = excluded.changes_requested,
    sub_issues_total = excluded.sub_issues_total,
    sub_issues_completed = excluded.sub_issues_completed,
    review_users = excluded.review_users,
    review_teams = excluded.review_teams,
    auto_merge_method = excluded.auto_merge_method,
    auto_merge_at = excluded.auto_merge_at,
    issue_info = excluded.issue_info,
    closing_pulls = excluded.closing_pulls,
    approving_bots = excluded.approving_bots,
    review_texts = excluded.review_texts,
    fetched_at = excluded.fetched_at;

-- name: GetActivity :one
-- GetActivity returns the activity a snooze compares from the details of the item with
-- item_id and whether it is a draft, or sql.ErrNoRows when no refresh has read them.
SELECT d.head_oid, d.commits, d.comments, d.review_decision, d.reviewed, d.approvals, d.changes_requested,
    i.is_draft
FROM item_details d
JOIN items i ON i.id = d.item_id
WHERE d.item_id = ?;

-- name: SetReviewRequests :exec
-- SetReviewRequests rewrites who the pull request with item_id asks to review, as GitHub
-- answered an edit to its review requests. Nothing changes when no refresh has read its
-- details.
UPDATE item_details SET review_users = ?, review_teams = ? WHERE item_id = ?;

-- name: CountOwnComment :exec
-- CountOwnComment counts one more comment on the conversation of the item with id, the one
-- the user just posted from the inbox, so its card says so before the next read of its
-- details, and a snooze taken right after counts it in its baseline (activityBaseline),
-- never as new activity that wakes it. An item whose details no refresh has read yet
-- is left as it is: the first read counts it.
UPDATE item_details SET comments = comments + 1 WHERE item_id = ?;
