-- name: UpsertLaunch :exec
-- UpsertLaunch stores a launch, replacing an earlier one for the same worktree of the same
-- clone.
INSERT INTO worktree_launches (clone_path, worktree, owner, name, kind, number, launched_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (clone_path, worktree) DO UPDATE SET
    owner = excluded.owner,
    name = excluded.name,
    kind = excluded.kind,
    number = excluded.number,
    launched_at = excluded.launched_at;

-- name: ListLaunches :many
-- ListLaunches returns every launch, oldest first; launches at the same time come in the
-- order they were first recorded.
SELECT clone_path, worktree, owner, name, kind, number, launched_at
FROM worktree_launches
ORDER BY launched_at, id;

-- name: DeleteLaunch :exec
-- DeleteLaunch removes the launch of a clone's worktree; a worktree with none is not an
-- error.
DELETE FROM worktree_launches WHERE clone_path = ? AND worktree = ?;
