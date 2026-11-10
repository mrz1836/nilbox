-- 0011_script_runs: every run from the Scripts tab, in the app or in the user's terminal.

-- One run. kind (release, hooks, checks, script) and status (running, succeeded, failed,
-- stopped, interrupted, orphaned) are checked in Go, so later kinds need no rebuild. dir is
-- the absolute folder, shown with ~; argv is the JSON argv as started, and command its
-- display form. runs_in is app or terminal. pid and pgid are the process and its group once
-- known, or 0, and proc_started_at is the process's own start time, which tells a live run
-- from a reused pid. result is JSON per kind: a release writes its intent (the expected
-- version, the branch, and the branch the clone was on) at the start, so a run cut short can
-- still be verified. seen_at clears the tab's red dot.
CREATE TABLE script_runs (
    id              INTEGER  PRIMARY KEY,
    kind            TEXT     NOT NULL CHECK (kind <> ''),
    title           TEXT     NOT NULL DEFAULT '',
    source          TEXT     NOT NULL DEFAULT '',
    repo_owner      TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    repo_name       TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    dir             TEXT     NOT NULL CHECK (dir <> ''),
    argv            TEXT     NOT NULL DEFAULT '[]',
    command         TEXT     NOT NULL DEFAULT '',
    runs_in         TEXT     NOT NULL DEFAULT 'app',
    status          TEXT     NOT NULL CHECK (status <> ''),
    exit_code       INTEGER,
    pid             INTEGER  NOT NULL DEFAULT 0 CHECK (pid >= 0),
    pgid            INTEGER  NOT NULL DEFAULT 0 CHECK (pgid >= 0),
    proc_started_at DATETIME,
    summary         TEXT     NOT NULL DEFAULT '',
    result          TEXT     NOT NULL DEFAULT '{}',
    log_bytes       INTEGER  NOT NULL DEFAULT 0,
    seen_at         DATETIME,
    started_at      DATETIME NOT NULL,
    finished_at     DATETIME
);

CREATE INDEX script_runs_started ON script_runs (started_at);
CREATE INDEX script_runs_status ON script_runs (status);
