-- name: RecordCopy :exec
-- RecordCopy stores the pull request a worktree's copied commits went into, replacing what
-- an earlier row of the same worktree said. A row for the same HEAD keeps when it was first
-- written.
INSERT INTO worktree_copies (
    clone_path, worktree_path, head, ref, repo_owner, repo_name, pr_number, copy_tip, commits, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (worktree_path) DO UPDATE SET
    clone_path = excluded.clone_path,
    ref = excluded.ref,
    repo_owner = excluded.repo_owner,
    repo_name = excluded.repo_name,
    pr_number = excluded.pr_number,
    copy_tip = excluded.copy_tip,
    commits = excluded.commits,
    updated_at = excluded.updated_at,
    created_at = CASE
        WHEN worktree_copies.head = excluded.head THEN worktree_copies.created_at
        ELSE excluded.created_at
    END,
    head = excluded.head;

-- name: ListCopies :many
-- ListCopies returns every worktree's copy record, in the order they were first written.
SELECT clone_path, worktree_path, head, ref, repo_owner, repo_name, pr_number, copy_tip, commits, created_at,
    updated_at
FROM worktree_copies
ORDER BY id;

-- name: DeleteCopy :exec
-- DeleteCopy removes a worktree's copy record; a worktree with none is not an error.
DELETE FROM worktree_copies WHERE worktree_path = ?;
