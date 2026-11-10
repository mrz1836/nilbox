-- name: InsertRun :one
-- InsertRun records a run as it starts, and returns its ID.
INSERT INTO script_runs (
    kind, title, source, repo_owner, repo_name, dir, argv, command, runs_in, status, result, started_at
) VALUES (
    sqlc.arg(kind), sqlc.arg(title), sqlc.arg(source), sqlc.arg(repo_owner), sqlc.arg(repo_name), sqlc.arg(dir),
    sqlc.arg(argv), sqlc.arg(command), sqlc.arg(runs_in), sqlc.arg(status), sqlc.arg(result), sqlc.arg(started_at)
)
RETURNING id;

-- name: SetRunProcess :exec
-- SetRunProcess records a run's process, its group, and when the process started.
UPDATE script_runs SET
    pid = sqlc.arg(pid),
    pgid = sqlc.arg(pgid),
    proc_started_at = sqlc.arg(proc_started_at)
WHERE id = sqlc.arg(id);

-- name: SetRunStatus :execrows
-- SetRunStatus changes the status and summary of a run that has not finished, as reconcile
-- does for a run from before the app restarted that is still going.
UPDATE script_runs SET
    status = sqlc.arg(status),
    summary = sqlc.arg(summary)
WHERE id = sqlc.arg(id) AND status IN ('running', 'orphaned');

-- name: FinishRun :execrows
-- FinishRun records how a run that has not finished ended. A run finished already is left
-- as it is.
UPDATE script_runs SET
    status = sqlc.arg(status),
    exit_code = sqlc.narg(exit_code),
    summary = sqlc.arg(summary),
    result = sqlc.arg(result),
    log_bytes = sqlc.arg(log_bytes),
    finished_at = sqlc.arg(finished_at)
WHERE id = sqlc.arg(id) AND status IN ('running', 'orphaned');

-- name: GetRun :one
-- GetRun returns one run.
SELECT id, kind, title, source, repo_owner, repo_name, dir, argv, command, runs_in, status, exit_code, pid, pgid,
       proc_started_at, summary, result, log_bytes, seen_at, started_at, finished_at
FROM script_runs
WHERE id = sqlc.arg(id);

-- name: ListRuns :many
-- ListRuns returns every run, newest first.
SELECT id, kind, title, source, repo_owner, repo_name, dir, argv, command, runs_in, status, exit_code, pid, pgid,
       proc_started_at, summary, result, log_bytes, seen_at, started_at, finished_at
FROM script_runs
ORDER BY started_at DESC, id DESC;

-- name: LiveRuns :many
-- LiveRuns returns every run that has not finished, oldest first.
SELECT id, kind, title, source, repo_owner, repo_name, dir, argv, command, runs_in, status, exit_code, pid, pgid,
       proc_started_at, summary, result, log_bytes, seen_at, started_at, finished_at
FROM script_runs
WHERE status IN ('running', 'orphaned')
ORDER BY id;

-- name: RunIDs :many
-- RunIDs returns the ID of every run.
SELECT id FROM script_runs ORDER BY id;

-- name: MarkRunSeen :execrows
-- MarkRunSeen records that the user has seen a run.
UPDATE script_runs SET seen_at = sqlc.arg(seen_at) WHERE id = sqlc.arg(id);

-- name: DeleteRuns :exec
-- DeleteRuns deletes the runs named that have finished; one still going is kept.
DELETE FROM script_runs WHERE status NOT IN ('running', 'orphaned') AND id IN (sqlc.slice(ids));
