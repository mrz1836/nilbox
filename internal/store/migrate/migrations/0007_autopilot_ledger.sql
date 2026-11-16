-- 0007_autopilot_ledger: what Autopilot's routines did with the items they matched.
--
-- The conventions are 0001_init's: DATETIME stamps the application writes in UTC, BOOLEAN
-- columns kept to 0 or 1, and NOCASE for GitHub logins. Autopilot owns every table here. A
-- row names its routine, which is deleted softly, so no row here goes with its routine;
-- one that names an item keeps a copy of it (its repository, kind, number, title, and
-- URL), so it still reads once the item is gone, and its item_id is set NULL then.

-- == autopilot ===========================================================================

-- One row per routine, item, and occurrence it fired for (fire_key, as the evaluator
-- names it), apart for a Dry run's record (dry_run), so a record of what a routine would
-- have done never blocks a real run. The row is the match's whole life: state is open
-- (waiting, blocked, queued, running, verifying) or closed (done, skipped, dismissed,
-- closed, baseline), checked in Go, so a later state needs no rebuild. blocked_by says
-- what an open row waits on, and reason why a closed one closed; detail is the words
-- beside either. Every change is a compare-and-set on state.
--
-- trace is the decision as it was when the routine matched (JSON, the evaluator's steps),
-- so "why did it run?" shows the facts then. steps holds the steps the match started with
-- and how each went (JSON), and step where it is in them: a routine saved with other
-- steps never changes a match that has started one. spec_hash names the routine's spec
-- the steps came from. head_oid is the head commit the match was decided on, and
-- auto_merge_at_match whether auto-merge was on then (NULL for an issue, or before it was
-- read). approved records the user's OK for a routine that asks first, manual a run the
-- user started by hand, and attempt how many runs the match has started. decided_at is
-- when the routine last decided it, and closed_at when it closed, NULL while open.
CREATE TABLE autopilot_matches (
    id                  INTEGER  PRIMARY KEY,
    routine_id          INTEGER  NOT NULL REFERENCES autopilot_routines (id),
    item_id             INTEGER  REFERENCES items (id) ON DELETE SET NULL,
    account_login       TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    repo_owner          TEXT     NOT NULL DEFAULT '',
    repo_name           TEXT     NOT NULL DEFAULT '',
    kind                TEXT     NOT NULL DEFAULT '',
    number              INTEGER  NOT NULL DEFAULT 0,
    title               TEXT     NOT NULL DEFAULT '',
    url                 TEXT     NOT NULL DEFAULT '',
    fire_key            TEXT     NOT NULL CHECK (fire_key <> ''),
    dry_run             BOOLEAN  NOT NULL DEFAULT 0 CHECK (dry_run IN (0, 1)),
    manual              BOOLEAN  NOT NULL DEFAULT 0 CHECK (manual IN (0, 1)),
    state               TEXT     NOT NULL CHECK (state <> ''),
    blocked_by          TEXT     NOT NULL DEFAULT '',
    reason              TEXT     NOT NULL DEFAULT '',
    detail              TEXT     NOT NULL DEFAULT '',
    trace               TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(trace)),
    steps               TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(steps)),
    step                INTEGER  NOT NULL DEFAULT 0 CHECK (step >= 0),
    spec_hash           TEXT     NOT NULL DEFAULT '',
    head_oid            TEXT     NOT NULL DEFAULT '',
    auto_merge_at_match BOOLEAN  CHECK (auto_merge_at_match IN (0, 1)),
    approved            BOOLEAN  NOT NULL DEFAULT 0 CHECK (approved IN (0, 1)),
    attempt             INTEGER  NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    created_at          DATETIME NOT NULL,
    updated_at          DATETIME NOT NULL,
    decided_at          DATETIME NOT NULL,
    closed_at           DATETIME,
    UNIQUE (routine_id, item_id, fire_key, dry_run)
);

-- Every cycle reads the open rows.
CREATE INDEX autopilot_matches_open ON autopilot_matches (state)
    WHERE state IN ('waiting', 'blocked', 'queued', 'running', 'verifying');

CREATE INDEX autopilot_matches_routine_item ON autopilot_matches (routine_id, item_id);

CREATE INDEX autopilot_matches_created ON autopilot_matches (created_at);

-- The items a routine leaves alone, since the user reversed what it did to them.
CREATE TABLE autopilot_hands_off (
    routine_id INTEGER  NOT NULL REFERENCES autopilot_routines (id),
    item_id    INTEGER  NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    since      DATETIME NOT NULL,
    why        TEXT     NOT NULL DEFAULT '',
    PRIMARY KEY (routine_id, item_id)
);

-- When Try again last reset a routine's count of tries on an item.
CREATE TABLE autopilot_tries (
    routine_id INTEGER  NOT NULL REFERENCES autopilot_routines (id),
    item_id    INTEGER  NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    reset_at   DATETIME NOT NULL,
    PRIMARY KEY (routine_id, item_id)
);

-- What a routine saw of an item when it first saw it, and since (JSON: the head commit
-- it first saw, and the comment count it last fired at), for the triggers that count
-- from then: New commits' first commit, and New comments.
CREATE TABLE autopilot_baselines (
    routine_id    INTEGER  NOT NULL REFERENCES autopilot_routines (id),
    item_id       INTEGER  NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    state         TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(state)),
    first_seen_at DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    PRIMARY KEY (routine_id, item_id)
);

-- One row per run a match started: an agent's, in the background or in the user's
-- terminal, or a GitHub action the app sent (runner), with everything the run view and the
-- stats read. status is queued, starting, running, stopping, or ended; outcome how it
-- ended, and verdict, for a run that pushed, what its commit's checks said. agent is the
-- agent's ID, github_action the action sent; script_run_id is the run engine's run, 0 for
-- none; session_id the sessions ledger's, and agent_session_id the agent's own.
-- head_before is the head commit at the start, run_commit the commit the run pushed, and
-- head_after the head at its end. budget_reserved is what the start reserved of the daily
-- cap, in budget_unit (usd or tokens), until the run reports cost (NULL when it reports
-- none) and its tokens. manual is a run the user started by hand, which counts toward no
-- cap or try; counted says it counts as a try. log_cleared says its log was cleared.
CREATE TABLE autopilot_runs (
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
    runner            TEXT     NOT NULL CHECK (runner IN ('terminal', 'background', 'github')),
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

-- One active run per item, whatever the engine's own checks say.
CREATE UNIQUE INDEX autopilot_runs_active_item ON autopilot_runs (item_id)
    WHERE status IN ('starting', 'running', 'stopping');

CREATE INDEX autopilot_runs_routine_started ON autopilot_runs (routine_id, started_at);

CREATE INDEX autopilot_runs_started ON autopilot_runs (started_at);

CREATE INDEX autopilot_runs_match ON autopilot_runs (match_id);
