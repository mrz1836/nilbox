-- name: InsertMatch :one
-- InsertMatch records a routine's match on an item for one occurrence, and returns its
-- ID, or sql.ErrNoRows when a row for that routine, item, occurrence, and Dry run is there
-- already: the row that makes the routine act once per occurrence.
INSERT INTO autopilot_matches (
    routine_id, item_id, account_login, repo_owner, repo_name, kind, number, title, url, fire_key, dry_run, manual,
    state, blocked_by, reason, detail, trace, steps, step, spec_hash, head_oid, auto_merge_at_match, approved, attempt,
    created_at, updated_at, decided_at, closed_at
) VALUES (
    sqlc.arg(routine_id), sqlc.arg(item_id), sqlc.arg(account_login), sqlc.arg(repo_owner), sqlc.arg(repo_name),
    sqlc.arg(kind), sqlc.arg(number), sqlc.arg(title), sqlc.arg(url), sqlc.arg(fire_key), sqlc.arg(dry_run),
    sqlc.arg(manual), sqlc.arg(state), sqlc.arg(blocked_by), sqlc.arg(reason), sqlc.arg(detail), sqlc.arg(trace),
    sqlc.arg(steps), sqlc.arg(step), sqlc.arg(spec_hash), sqlc.arg(head_oid), sqlc.narg(auto_merge_at_match),
    sqlc.arg(approved), sqlc.arg(attempt), sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.arg(decided_at),
    sqlc.narg(closed_at)
)
ON CONFLICT (routine_id, item_id, fire_key, dry_run) DO NOTHING
RETURNING id;

-- name: GetMatch :one
-- GetMatch returns one match, or sql.ErrNoRows.
SELECT * FROM autopilot_matches WHERE id = sqlc.arg(id);

-- name: ListOpenMatches :many
-- ListOpenMatches returns every open match, oldest first.
SELECT * FROM autopilot_matches
WHERE state IN ('waiting', 'blocked', 'queued', 'running', 'verifying')
ORDER BY id;

-- name: ListRoutineMatchKeys :many
-- ListRoutineMatchKeys returns the occurrence each of a routine's matches is for, with its
-- state, why it closed, how many runs it started, where it is in its steps, and its steps:
-- what tells a match that is new from one recorded, and one that never ran from one that
-- did.
SELECT id, item_id, fire_key, dry_run, state, reason, attempt, step, steps FROM autopilot_matches
WHERE routine_id = sqlc.arg(routine_id) AND item_id IS NOT NULL;

-- name: ListMatchKeys :many
-- ListMatchKeys is ListRoutineMatchKeys for every routine, in one read: the ledger's keys the
-- engine keeps in memory between its cycles, built again from here when it takes the lock.
SELECT routine_id, id, item_id, fire_key, dry_run, state, reason, attempt, step, steps FROM autopilot_matches
WHERE item_id IS NOT NULL;

-- name: CountLedgerKeys :one
-- CountLedgerKeys counts the matches with an item and the baselines, which the engine's keys
-- in memory hold one each of: counts that differ say something besides the engine deleted
-- rows, as the inbox's prune of an old item takes its baselines with it and leaves its
-- matches without it.
SELECT
    CAST((SELECT COUNT(*) FROM autopilot_matches WHERE item_id IS NOT NULL) AS INTEGER) AS matches,
    CAST((SELECT COUNT(*) FROM autopilot_baselines) AS INTEGER) AS baselines;

-- name: ListItemMatches :many
-- ListItemMatches returns an item's matches, newest first.
SELECT * FROM autopilot_matches
WHERE item_id = sqlc.arg(item_id)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: ListClosedMatches :many
-- ListClosedMatches returns the matches in state that closed for reason with detail, at
-- or after since, oldest first.
SELECT * FROM autopilot_matches
WHERE state = sqlc.arg(state) AND reason = sqlc.arg(reason) AND detail = sqlc.arg(detail)
    AND closed_at >= sqlc.arg(since)
ORDER BY id;

-- name: ListLatestItemMatches :many
-- ListLatestItemMatches returns, for each item among ids, its newest match that did
-- something there or waits to: open, closed as done, skipped, or dismissed, or closed
-- after one of its steps ran (a reply sent, then a merge no longer offered); never a
-- baseline or one that closed because the item or the routine moved on before it acted.
-- Newest first.
SELECT m.* FROM autopilot_matches AS m
WHERE m.id IN (
    SELECT MAX(l.id) FROM autopilot_matches AS l
    WHERE l.item_id IN (sqlc.slice(ids))
        AND (
            l.state IN ('waiting', 'blocked', 'queued', 'running', 'verifying', 'done', 'skipped', 'dismissed')
            OR (l.state = 'closed' AND EXISTS (
                SELECT 1 FROM json_each(l.steps) AS s WHERE json_extract(s.value, '$.status') = 'done'
            ))
        )
    GROUP BY l.item_id
)
ORDER BY m.id DESC;

-- name: ListRoutineFigures :many
-- ListRoutineFigures returns, for each routine with a match that did something or waits to
-- (as ListLatestItemMatches counts them), how many it recorded since since, real ones and
-- a Dry run's apart, and how many wait for the user: each routine's line in the routine
-- list.
SELECT
    routine_id,
    CAST(SUM(CASE WHEN dry_run = 0 AND created_at >= sqlc.arg(since) THEN 1 ELSE 0 END) AS INTEGER) AS today,
    CAST(SUM(CASE WHEN dry_run = 1 AND created_at >= sqlc.arg(since) THEN 1 ELSE 0 END) AS INTEGER) AS would_run_today,
    CAST(SUM(CASE WHEN state = 'waiting' THEN 1 ELSE 0 END) AS INTEGER) AS waiting
FROM autopilot_matches AS m
WHERE m.state IN ('waiting', 'blocked', 'queued', 'running', 'verifying', 'done', 'skipped', 'dismissed')
    OR (m.state = 'closed' AND EXISTS (
        SELECT 1 FROM json_each(m.steps) AS s WHERE json_extract(s.value, '$.status') = 'done'
    ))
GROUP BY routine_id;

-- name: ListRoutineLastRuns :many
-- ListRoutineLastRuns returns, for each routine with a match that ran (done, or closed
-- after one of its steps ran; in Dry run, would have), the ID of the one that ran last: the
-- one that closed last, since a match runs as it closes, which can be long after it was
-- recorded (one that waited for the user, or under Pause). SQLite takes a bare column of a
-- query with one max() from the row that holds the max, which closed_last only selects.
SELECT routine_id, id, MAX(closed_at) AS closed_last
FROM autopilot_matches AS m
WHERE m.closed_at IS NOT NULL AND (
    m.state = 'done'
    OR (m.state = 'closed' AND EXISTS (
        SELECT 1 FROM json_each(m.steps) AS s WHERE json_extract(s.value, '$.status') = 'done'
    ))
)
GROUP BY routine_id;

-- name: ListMatchHeads :many
-- ListMatchHeads returns when each match among ids closed, and whether it is a Dry run's.
SELECT id, dry_run, closed_at FROM autopilot_matches WHERE id IN (sqlc.slice(ids));

-- name: ListMatchesFrom :many
-- ListMatchesFrom returns the matches recorded from the match first_id on and before the
-- match before_id, newest first: a stretch of the Activity the window has loaded, read
-- again.
SELECT * FROM autopilot_matches
WHERE id >= sqlc.arg(first_id) AND id < sqlc.arg(before_id)
ORDER BY id DESC
LIMIT sqlc.arg(row_limit);

-- name: ListMatchesBefore :many
-- ListMatchesBefore returns the matches recorded before the match before, newest first: a
-- page of the Activity.
SELECT * FROM autopilot_matches
WHERE id < sqlc.arg(before)
ORDER BY id DESC
LIMIT sqlc.arg(row_limit);

-- name: UpdateMatch :execrows
-- UpdateMatch writes a match's next state, only while it is still in the state it was
-- read in: a change that loses a race changes nothing.
UPDATE autopilot_matches SET
    state = sqlc.arg(state),
    blocked_by = sqlc.arg(blocked_by),
    reason = sqlc.arg(reason),
    detail = sqlc.arg(detail),
    steps = sqlc.arg(steps),
    step = sqlc.arg(step),
    spec_hash = sqlc.arg(spec_hash),
    approved = sqlc.arg(approved),
    attempt = sqlc.arg(attempt),
    updated_at = sqlc.arg(updated_at),
    decided_at = sqlc.arg(decided_at),
    closed_at = sqlc.narg(closed_at)
WHERE id = sqlc.arg(id) AND state = sqlc.arg(was);

-- name: ListPrunableMatches :many
-- ListPrunableMatches returns the closed matches closed before cutoff, with their items:
-- those the retention may delete once their item is known to be done with.
SELECT id, item_id FROM autopilot_matches
WHERE state IN ('done', 'skipped', 'dismissed', 'closed', 'baseline') AND closed_at < sqlc.arg(cutoff);

-- name: DeleteMatches :execrows
-- DeleteMatches deletes matches, and their runs with them.
DELETE FROM autopilot_matches WHERE id IN (sqlc.slice(ids));

-- name: ClearMatchTraces :exec
-- ClearMatchTraces clears what Clear history clears of matches that still block an
-- occurrence: the trace and the words, keeping the row, so nothing fires again.
UPDATE autopilot_matches SET trace = '[]', detail = '' WHERE id IN (sqlc.slice(ids));

-- name: ClearRunSummaries :exec
-- ClearRunSummaries clears the summaries and errors of the runs of matches that still
-- block an occurrence.
UPDATE autopilot_runs SET summary = '', error = '' WHERE match_id IN (sqlc.slice(ids));

-- name: ListLedgerItems :many
-- ListLedgerItems returns every item a match or a run refers to: those the inbox keeps.
SELECT item_id FROM autopilot_matches WHERE item_id IS NOT NULL
UNION
SELECT item_id FROM autopilot_runs WHERE item_id IS NOT NULL;

-- name: InsertRun :one
-- InsertRun records a run a match starts, and returns its ID. The one-active-run index
-- refuses a second active run on the item.
INSERT INTO autopilot_runs (
    match_id, routine_id, item_id, repo_owner, repo_name, kind, number, title, url, step, runner, agent,
    github_action, status, preset, head_before, budget_reserved, budget_unit, manual, counted, queued_at,
    started_at, updated_at
) VALUES (
    sqlc.arg(match_id), sqlc.arg(routine_id), sqlc.arg(item_id), sqlc.arg(repo_owner), sqlc.arg(repo_name),
    sqlc.arg(kind), sqlc.arg(number), sqlc.arg(title), sqlc.arg(url), sqlc.arg(step), sqlc.arg(runner),
    sqlc.arg(agent), sqlc.arg(github_action), sqlc.arg(status), sqlc.arg(preset), sqlc.arg(head_before),
    sqlc.arg(budget_reserved), sqlc.arg(budget_unit), sqlc.arg(manual), sqlc.arg(counted), sqlc.arg(queued_at),
    sqlc.narg(started_at), sqlc.arg(updated_at)
)
RETURNING id;

-- name: GetRun :one
-- GetRun returns one run, or sql.ErrNoRows.
SELECT * FROM autopilot_runs WHERE id = sqlc.arg(id);

-- name: GetRunBySession :one
-- GetRunBySession returns the newest run recorded as the session session_id, or
-- sql.ErrNoRows: the run view of a session's Watch.
SELECT * FROM autopilot_runs WHERE session_id = sqlc.arg(session_id) AND session_id != ''
ORDER BY id DESC LIMIT 1;

-- name: ListActiveRuns :many
-- ListActiveRuns returns the runs starting, running, or stopping, oldest first.
SELECT * FROM autopilot_runs WHERE status IN ('starting', 'running', 'stopping') ORDER BY id;

-- name: ListMatchRuns :many
-- ListMatchRuns returns the runs of the matches among ids, oldest first.
SELECT * FROM autopilot_runs WHERE match_id IN (sqlc.slice(ids)) ORDER BY id;

-- name: SetRunUsage :execrows
-- SetRunUsage records an agent's run's tokens so far, while it goes.
UPDATE autopilot_runs SET
    tokens_in = sqlc.arg(tokens_in),
    tokens_out = sqlc.arg(tokens_out),
    cache_read_tokens = sqlc.arg(cache_read_tokens),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status <> 'ended';

-- name: MarkRunLogsCleared :exec
-- MarkRunLogsCleared records that the agents' runs that ended lost their logs, as Clear
-- history clears them.
UPDATE autopilot_runs SET log_cleared = 1 WHERE runner = 'background' AND status = 'ended';

-- name: SetRunStatus :execrows
-- SetRunStatus moves a run from one status to the next.
UPDATE autopilot_runs SET status = sqlc.arg(status), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status = sqlc.arg(was);

-- name: RecordRunLaunch :exec
-- RecordRunLaunch records what an agent's launch started for a run not ended yet: the
-- session the sessions ledger records, the agent, the profile, the model, and the effort
-- it resolved to, the clone and the worktree, the command line shown, and, for a run in
-- the background, the run engine's run.
UPDATE autopilot_runs SET
    session_id = sqlc.arg(session_id),
    script_run_id = sqlc.arg(script_run_id),
    agent = sqlc.arg(agent),
    profile = sqlc.arg(profile),
    model = sqlc.arg(model),
    effort = sqlc.arg(effort),
    clone_path = sqlc.arg(clone_path),
    worktree = sqlc.arg(worktree),
    display_command = sqlc.arg(display_command),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status <> 'ended';

-- name: GetRunScript :one
-- GetRunScript returns the run engine's run a run started, 0 for none yet, and how the run
-- is recorded: the runs a script step's runner stops and reads the end of.
SELECT id, script_run_id, status, outcome FROM autopilot_runs WHERE id = sqlc.arg(id);

-- name: GetBackgroundRunTokens :one
-- GetBackgroundRunTokens returns the tokens an agent's run in the background reported so
-- far, by the run engine's run it started: what its session's panel counts.
SELECT tokens_in, tokens_out, cache_read_tokens FROM autopilot_runs
WHERE script_run_id = sqlc.arg(script_run_id) AND runner = 'background'
ORDER BY id DESC
LIMIT 1;

-- name: ListActiveScriptRuns :many
-- ListActiveScriptRuns returns the script steps' runs starting, running, or stopping, with
-- the run engine's run each started: what Stop all stops.
SELECT id, script_run_id FROM autopilot_runs
WHERE runner = 'script' AND status IN ('starting', 'running', 'stopping')
ORDER BY id;

-- name: ListRoutineWeekRuns :many
-- ListRoutineWeekRuns returns a routine's runs started since a moment, each with its step
-- and its match's steps, which say what kind of step it ran and how the match went on:
-- what its week's stats count.
SELECT r.id, r.match_id, r.step, r.runner, r.github_action, r.status, r.outcome, r.verdict, r.manual,
       r.cost, r.cost_estimated, r.tokens_in, r.tokens_out, r.cache_read_tokens, r.started_at,
       m.steps AS match_steps
FROM autopilot_runs AS r
JOIN autopilot_matches AS m ON m.id = r.match_id
WHERE r.routine_id = sqlc.arg(routine_id) AND r.started_at >= sqlc.arg(since)
ORDER BY r.id;

-- name: EndRun :execrows
-- EndRun records how a run ended, while it is not ended yet.
UPDATE autopilot_runs SET
    status = 'ended',
    outcome = sqlc.arg(outcome),
    run_commit = sqlc.arg(run_commit),
    head_after = sqlc.arg(head_after),
    cost = sqlc.narg(cost),
    cost_estimated = sqlc.arg(cost_estimated),
    tokens_in = sqlc.arg(tokens_in),
    tokens_out = sqlc.arg(tokens_out),
    cache_read_tokens = sqlc.arg(cache_read_tokens),
    turns = sqlc.arg(turns),
    summary = sqlc.arg(summary),
    error = sqlc.arg(error),
    counted = sqlc.arg(counted),
    finished_at = sqlc.arg(finished_at),
    updated_at = sqlc.arg(finished_at)
WHERE id = sqlc.arg(id) AND status <> 'ended';

-- name: ReviseRunEnd :execrows
-- ReviseRunEnd records how a run ended, as its runner told, over the interrupted end the
-- engine recorded for it once it found the run gone before that word came.
UPDATE autopilot_runs SET
    status = 'ended',
    outcome = sqlc.arg(outcome),
    run_commit = sqlc.arg(run_commit),
    head_after = sqlc.arg(head_after),
    cost = sqlc.narg(cost),
    cost_estimated = sqlc.arg(cost_estimated),
    tokens_in = sqlc.arg(tokens_in),
    tokens_out = sqlc.arg(tokens_out),
    cache_read_tokens = sqlc.arg(cache_read_tokens),
    turns = sqlc.arg(turns),
    summary = sqlc.arg(summary),
    error = sqlc.arg(error),
    counted = sqlc.arg(counted),
    finished_at = sqlc.arg(finished_at),
    updated_at = sqlc.arg(finished_at)
WHERE id = sqlc.arg(id) AND status = 'ended' AND outcome = 'interrupted';

-- name: SetRunVerdict :execrows
-- SetRunVerdict records what Wait for the checks found of a run that ended: its verdict,
-- and when it was reached.
UPDATE autopilot_runs SET
    verdict = sqlc.arg(verdict),
    verified_at = sqlc.narg(verified_at),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status = 'ended';

-- name: ListRunsSince :many
-- ListRunsSince returns the runs queued since a moment, or still active, for the daily
-- counts, the daily cap, and the pace of the GitHub actions.
SELECT id, match_id, routine_id, item_id, runner, status, outcome, manual, budget_reserved, budget_unit, cost,
       tokens_in, tokens_out, cache_read_tokens, queued_at, started_at
FROM autopilot_runs
WHERE queued_at >= sqlc.arg(since) OR status IN ('starting', 'running', 'stopping');

-- name: ListItemRuns :many
-- ListItemRuns returns the runs on the items among ids, oldest first, for the routines'
-- history of them: their tries, their last outcome, and the commits they pushed.
SELECT id, match_id, routine_id, item_id, runner, status, outcome, manual, counted, run_commit, head_after,
       queued_at, finished_at
FROM autopilot_runs
WHERE item_id IN (sqlc.slice(ids))
ORDER BY id;

-- name: ListTries :many
-- ListTries returns when Try again last reset each of a routine's counts.
SELECT item_id, reset_at FROM autopilot_tries WHERE routine_id = sqlc.arg(routine_id);

-- name: ListRoutinesTries :many
-- ListRoutinesTries is ListTries for each of the routines among ids, in one read.
SELECT routine_id, item_id, reset_at FROM autopilot_tries WHERE routine_id IN (sqlc.slice(ids));

-- name: UpsertTries :exec
-- UpsertTries records that Try again reset a routine's count of tries on an item.
INSERT INTO autopilot_tries (routine_id, item_id, reset_at)
VALUES (sqlc.arg(routine_id), sqlc.arg(item_id), sqlc.arg(reset_at))
ON CONFLICT (routine_id, item_id) DO UPDATE SET reset_at = excluded.reset_at;

-- name: ListBaselines :many
-- ListBaselines returns what a routine saw of each item when it first saw it, and since.
SELECT item_id, state, first_seen_at FROM autopilot_baselines WHERE routine_id = sqlc.arg(routine_id);

-- name: ListAllBaselines :many
-- ListAllBaselines is ListBaselines for every routine, in one read, as ListMatchKeys is.
SELECT routine_id, item_id, state FROM autopilot_baselines;

-- name: InsertBaseline :execrows
-- InsertBaseline records what a routine first saw of an item, unless it saw it before, and
-- says whether it did.
INSERT INTO autopilot_baselines (routine_id, item_id, state, first_seen_at, updated_at)
VALUES (sqlc.arg(routine_id), sqlc.arg(item_id), sqlc.arg(state), sqlc.arg(first_seen_at), sqlc.arg(first_seen_at))
ON CONFLICT (routine_id, item_id) DO NOTHING;

-- name: UpdateBaseline :execrows
-- UpdateBaseline records what a routine last counted of an item, and says whether it did.
UPDATE autopilot_baselines SET state = sqlc.arg(state), updated_at = sqlc.arg(updated_at)
WHERE routine_id = sqlc.arg(routine_id) AND item_id = sqlc.arg(item_id);

-- name: ListHandsOff :many
-- ListHandsOff returns the items a routine leaves alone.
SELECT item_id, since, why FROM autopilot_hands_off WHERE routine_id = sqlc.arg(routine_id);

-- name: ListRoutinesHandsOff :many
-- ListRoutinesHandsOff is ListHandsOff for each of the routines among ids, in one read.
SELECT routine_id, item_id FROM autopilot_hands_off WHERE routine_id IN (sqlc.slice(ids));

-- name: InsertHandsOff :exec
-- InsertHandsOff has a routine leave an item alone from now on.
INSERT INTO autopilot_hands_off (routine_id, item_id, since, why)
VALUES (sqlc.arg(routine_id), sqlc.arg(item_id), sqlc.arg(since), sqlc.arg(why))
ON CONFLICT (routine_id, item_id) DO NOTHING;

-- name: DeleteHandsOff :execrows
-- DeleteHandsOff lets a routine act on an item again.
DELETE FROM autopilot_hands_off WHERE routine_id = sqlc.arg(routine_id) AND item_id = sqlc.arg(item_id);

-- name: DeleteGoneRoutines :execrows
-- DeleteGoneRoutines removes for good the routines deleted before cutoff that nothing of
-- the history refers to, with what they kept of their items.
DELETE FROM autopilot_routines
WHERE deleted_at IS NOT NULL AND deleted_at < sqlc.arg(cutoff)
  AND NOT EXISTS (SELECT 1 FROM autopilot_matches m WHERE m.routine_id = autopilot_routines.id)
  AND NOT EXISTS (SELECT 1 FROM autopilot_runs r WHERE r.routine_id = autopilot_routines.id);

-- name: DeleteDeletedRoutinesState :execrows
-- DeleteDeletedRoutinesState forgets what deleted routines first saw of their items, their
-- baselines, and says how many it forgot; DeleteDeletedRoutinesTries and
-- DeleteDeletedRoutinesHandsOff forget the rest of what they kept.
DELETE FROM autopilot_baselines
WHERE routine_id IN (SELECT id FROM autopilot_routines WHERE deleted_at IS NOT NULL);

-- name: DeleteDeletedRoutinesTries :exec
-- DeleteDeletedRoutinesTries forgets deleted routines' resets of their tries.
DELETE FROM autopilot_tries
WHERE routine_id IN (SELECT id FROM autopilot_routines WHERE deleted_at IS NOT NULL);

-- name: DeleteDeletedRoutinesHandsOff :exec
-- DeleteDeletedRoutinesHandsOff forgets the items deleted routines left alone.
DELETE FROM autopilot_hands_off
WHERE routine_id IN (SELECT id FROM autopilot_routines WHERE deleted_at IS NOT NULL);

-- name: ListRoutineNames :many
-- ListRoutineNames returns every routine's name, the deleted ones included, for the
-- history that names them.
SELECT id, name, deleted_at FROM autopilot_routines;

-- name: ListRoutineAsked :many
-- ListRoutineAsked returns a routine's real matches the user answered, approved or skipped,
-- that closed after since, newest first: what graduating counts its clean approvals in a
-- row from.
SELECT id, state, reason, approved, steps, closed_at FROM autopilot_matches
WHERE routine_id = sqlc.arg(routine_id) AND dry_run = 0 AND manual = 0 AND closed_at IS NOT NULL
    AND closed_at > sqlc.arg(since) AND (approved = 1 OR state = 'dismissed')
ORDER BY closed_at DESC, id DESC
LIMIT sqlc.arg(row_limit);
