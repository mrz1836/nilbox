-- 0016_autopilot_script_runs: a routine's Run one of my scripts is a run, as an agent's is.
--
-- A run of a match's step names who ran it (runner): an agent in the background or in the
-- user's terminal, a GitHub action the app sent, and now one of the user's own Repo
-- scripts, which Repo scripts runs in the app (script, its script_run_id the run engine's
-- run). SQLite can't change a CHECK, so the table is made again with the wider one, every
-- row copied as it is, its indexes made again, and one added on the run engine's run. Nothing refers to autopilot_runs, so it
-- can go and come back inside this migration's transaction. Autopilot owns the table.

-- == autopilot ===========================================================================

CREATE TABLE autopilot_runs_next (
    id                INTEGER  PRIMARY KEY,
    match_id          INTEGER  NOT NULL REFERENCES autopilot_matches (id) ON DELETE CASCADE,
    routine_id        INTEGER  NOT NULL REFERENCES autopilot_routines (id),
    item_id           INTEGER  REFERENCES items (id) ON DELETE SET NULL,
    repo_owner        TEXT     NOT NULL DEFAULT '',
    repo_name         TEXT     NOT NULL DEFAULT '',
    kind              TEXT     NOT NULL DEFAULT '',
    number            INTEGER  NOT NULL DEFAULT 0,
    title             TEXT     NOT NULL DEFAULT '',
    url               TEXT     NOT NULL DEFAULT '',
    step              INTEGER  NOT NULL DEFAULT 0 CHECK (step >= 0),
    runner            TEXT     NOT NULL CHECK (runner IN ('terminal', 'background', 'github', 'script')),
    agent             TEXT     NOT NULL DEFAULT '',
    github_action     TEXT     NOT NULL DEFAULT '',
    script_run_id     INTEGER  NOT NULL DEFAULT 0,
    session_id        TEXT     NOT NULL DEFAULT '',
    agent_session_id  TEXT     NOT NULL DEFAULT '',
    status            TEXT     NOT NULL CHECK (status <> ''),
    outcome           TEXT     NOT NULL DEFAULT '',
    verdict           TEXT     NOT NULL DEFAULT '',
    profile           TEXT     NOT NULL DEFAULT '',
    model             TEXT     NOT NULL DEFAULT '',
    effort            TEXT     NOT NULL DEFAULT '',
    preset            TEXT     NOT NULL DEFAULT '',
    clone_path        TEXT     NOT NULL DEFAULT '',
    worktree          TEXT     NOT NULL DEFAULT '',
    head_before       TEXT     NOT NULL DEFAULT '',
    run_commit        TEXT     NOT NULL DEFAULT '',
    head_after        TEXT     NOT NULL DEFAULT '',
    display_command   TEXT     NOT NULL DEFAULT '',
    budget_reserved   REAL     NOT NULL DEFAULT 0 CHECK (budget_reserved >= 0),
    budget_unit       TEXT     NOT NULL DEFAULT '',
    cost              REAL,
    cost_estimated    BOOLEAN  NOT NULL DEFAULT 0 CHECK (cost_estimated IN (0, 1)),
    tokens_in         INTEGER  NOT NULL DEFAULT 0,
    tokens_out        INTEGER  NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER  NOT NULL DEFAULT 0,
    turns             INTEGER  NOT NULL DEFAULT 0,
    summary           TEXT     NOT NULL DEFAULT '',
    error             TEXT     NOT NULL DEFAULT '',
    manual            BOOLEAN  NOT NULL DEFAULT 0 CHECK (manual IN (0, 1)),
    counted           BOOLEAN  NOT NULL DEFAULT 1 CHECK (counted IN (0, 1)),
    log_cleared       BOOLEAN  NOT NULL DEFAULT 0 CHECK (log_cleared IN (0, 1)),
    queued_at         DATETIME NOT NULL,
    started_at        DATETIME,
    finished_at       DATETIME,
    verified_at       DATETIME,
    updated_at        DATETIME NOT NULL
);

INSERT INTO autopilot_runs_next (
    id, match_id, routine_id, item_id, repo_owner, repo_name, kind, number, title, url, step, runner, agent,
    github_action, script_run_id, session_id, agent_session_id, status, outcome, verdict, profile, model, effort,
    preset, clone_path, worktree, head_before, run_commit, head_after, display_command, budget_reserved, budget_unit,
    cost, cost_estimated, tokens_in, tokens_out, cache_read_tokens, turns, summary, error, manual, counted, log_cleared,
    queued_at, started_at, finished_at, verified_at, updated_at
)
SELECT
    id, match_id, routine_id, item_id, repo_owner, repo_name, kind, number, title, url, step, runner, agent,
    github_action, script_run_id, session_id, agent_session_id, status, outcome, verdict, profile, model, effort,
    preset, clone_path, worktree, head_before, run_commit, head_after, display_command, budget_reserved, budget_unit,
    cost, cost_estimated, tokens_in, tokens_out, cache_read_tokens, turns, summary, error, manual, counted, log_cleared,
    queued_at, started_at, finished_at, verified_at, updated_at
FROM autopilot_runs;

DROP TABLE autopilot_runs;

ALTER TABLE autopilot_runs_next RENAME TO autopilot_runs;

-- One active run per item, whatever the engine's own checks say.
CREATE UNIQUE INDEX autopilot_runs_active_item ON autopilot_runs (item_id)
    WHERE status IN ('starting', 'running', 'stopping');

CREATE INDEX autopilot_runs_routine_started ON autopilot_runs (routine_id, started_at);

CREATE INDEX autopilot_runs_started ON autopilot_runs (started_at);

CREATE INDEX autopilot_runs_match ON autopilot_runs (match_id);

-- The run engine's run a run started, by which a background agent's session finds its
-- run's tokens for the session panel.
CREATE INDEX autopilot_runs_script_run ON autopilot_runs (script_run_id) WHERE script_run_id > 0;
