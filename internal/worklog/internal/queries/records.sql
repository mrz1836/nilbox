-- name: RecordWorktree :exec
-- RecordWorktree stores where a clone's worktree for an item is, and which item it is for.
-- A worktree recorded already keeps when it was made and its sessions, and the commit it
-- was made at, fetched from GitHub, unless this record names one.
INSERT INTO worktree_records (
    clone_path, worktree, path, branch, owner, name, kind, number, agent, created_at, launches, fetched_head
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?, 0, ?)
ON CONFLICT (clone_path, worktree) DO UPDATE SET
    path = excluded.path,
    branch = excluded.branch,
    owner = excluded.owner,
    name = excluded.name,
    kind = excluded.kind,
    number = excluded.number,
    fetched_head = CASE
        WHEN excluded.fetched_head <> '' THEN excluded.fetched_head
        ELSE worktree_records.fetched_head
    END;

-- name: TouchWorktree :execrows
-- TouchWorktree records a session started in a clone's worktree: its agent and when, and
-- one more launch. No row changes when the worktree is not recorded.
UPDATE worktree_records SET
    agent = ?,
    last_agent_at = ?,
    launches = launches + 1
WHERE clone_path = ? AND worktree = ?;

-- name: ListWorktrees :many
-- ListWorktrees returns every recorded worktree, oldest first; worktrees recorded at the
-- same time come in the order they were first recorded.
SELECT clone_path, worktree, path, branch, owner, name, kind, number, agent, created_at, last_agent_at, launches,
    fetched_head
FROM worktree_records
ORDER BY created_at, id;

-- name: DeleteWorktree :exec
-- DeleteWorktree removes the record of a clone's worktree; a worktree with none is not an
-- error.
DELETE FROM worktree_records WHERE clone_path = ? AND worktree = ?;
