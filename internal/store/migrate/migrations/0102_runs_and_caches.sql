-- 0102_runs_and_caches: the worktrees the app made, the agent sessions it followed, the
-- sheets' caches, the profile pictures, the tools it installed, and the runs it started.
--
-- The conventions are 0100_settings's.

-- == worklog =============================================================================

-- The worktrees the app made for pull requests and issues, one per item per clone, which
-- sessions of every agent share.
--
-- clone_path is the clone the worktree belongs to, as the launcher resolved it; worktree
-- is the item's worktree name (pr-<n> or issue-<n>), and owner, name, kind, and number the
-- item. path is the worktree's directory, and branch the branch it was made on
-- (worktree-pr-<n> or worktree-issue-<n>). A path of '' means the worktree is
-- <clone_path>/.claude/worktrees/<worktree>, where Claude Code made worktrees before the
-- app made them itself. agent is the agent of the latest session, created_at when the
-- worktree was made or first recorded, last_agent_at when the latest session started
-- (NULL before any did), and launches how many sessions started there.
--
-- fetched_head is the commit the app made the worktree at, just fetched from GitHub: a pull
-- request's head (refs/pull/<n>/head, which the fetch puts on the item's own branch, not on
-- a remote-tracking branch), or the default branch an issue's worktree starts from; '' for
-- a worktree the app found, or added on a branch left from an earlier one. Every commit it
-- reaches came from GitHub, so the Worktrees view counts only the commits past it as not on
-- GitHub, and a pull request that moves on after the launch leaves the worktree's own
-- commits counted as none. A later record of the same worktree that knows no such commit
-- keeps the one stored.
--
-- The worktrees service checks every row against git each time it lists or sweeps: a row
-- whose worktree is gone is removed, and a worktree on an item's branch with no row is
-- recorded.
CREATE TABLE worktree_records (
    id            INTEGER  PRIMARY KEY,
    clone_path    TEXT     NOT NULL CHECK (clone_path <> ''),
    worktree      TEXT     NOT NULL CHECK (worktree <> ''),
    path          TEXT     NOT NULL DEFAULT '',
    branch        TEXT     NOT NULL DEFAULT '',
    owner         TEXT     NOT NULL COLLATE NOCASE CHECK (owner <> ''),
    name          TEXT     NOT NULL COLLATE NOCASE CHECK (name <> ''),
    kind          TEXT     NOT NULL CHECK (kind IN ('pr', 'issue')),
    number        INTEGER  NOT NULL CHECK (number > 0),
    agent         TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    last_agent_at DATETIME,
    launches      INTEGER  NOT NULL DEFAULT 0 CHECK (launches >= 0),
    fetched_head  TEXT     NOT NULL DEFAULT '',
    UNIQUE (clone_path, worktree)
);

-- The worktrees the app cleaned up, newest kept.
--
-- Cleaning up removes a finished worktree, and its own branch, once its pull request merged
-- or its issue closed: by hand from the Worktrees view, or on its own when automatic
-- cleanup is on. Each one is recorded here so the view can show what was removed and when,
-- across restarts, since a toast is easy to miss. automatic tells the sweep from the user.
-- display_path is the worktree's path with ~ for home, as the view shows it; head is the
-- commit it had checked out, which was on GitHub. Only the newest rows are kept.
CREATE TABLE worktree_cleanups (
    id             INTEGER  PRIMARY KEY,
    cleaned_at     DATETIME NOT NULL,
    automatic      BOOLEAN  NOT NULL DEFAULT 0 CHECK (automatic IN (0, 1)),
    repo           TEXT     NOT NULL CHECK (repo <> ''),
    worktree       TEXT     NOT NULL CHECK (worktree <> ''),
    display_path   TEXT     NOT NULL DEFAULT '',
    deleted_branch TEXT     NOT NULL DEFAULT '',
    head           TEXT     NOT NULL DEFAULT '',
    item_kind      TEXT     NOT NULL DEFAULT '' CHECK (item_kind IN ('', 'pr', 'issue')),
    item_owner     TEXT     NOT NULL DEFAULT '',
    item_name      TEXT     NOT NULL DEFAULT '',
    item_number    INTEGER  NOT NULL DEFAULT 0 CHECK (item_number >= 0),
    item_title     TEXT     NOT NULL DEFAULT '',
    item_url       TEXT     NOT NULL DEFAULT '',
    item_state     TEXT     NOT NULL DEFAULT '' CHECK (item_state IN ('', 'open', 'merged', 'closed'))
);

CREATE INDEX worktree_cleanups_cleaned_at ON worktree_cleanups (cleaned_at);

-- == sessions ============================================================================

-- One coding-agent session started from an item, followed from the moment its terminal
-- opens until the agent quits. id is the app's own UUID, minted before the launch;
-- agent_session_id is the agent's own: the Claude Code session, which the launch passes as
-- --session-id, or the Codex thread, found once the agent starts, and '' until then. A
-- session resumed from another names it in resumed_from and shares its agent_session_id.
-- The state is what the agent last reported (see the sessions package), and last_message
-- is the start of what it last said, kept only on this Mac. pid is the agent's process
-- once known, or 0.
--
-- permission summarizes the permissions the session started with, as the launch worded
-- them ("full access", "plan mode (outside contributor)"). worktree_action is what the
-- launch did to the item's worktree (resume, adopt, attach, or create), or '' for a
-- session in the clone. launch_notice says what the launch worked around, such as a
-- worktree started from the last fetched copy of the default branch, or '' for nothing.
--
-- run_id is the script_runs ID of the background run an Autopilot routine started, whose
-- session this is, or 0 for a session a launch opened in the user's terminal. Such a
-- session's state comes from its run's own stream, not from an observer, so the Watcher
-- leaves it to the run; the row's pill and panel, Rereads, and LastWords follow it as they
-- follow any session.
--
-- github_event is what the inbox learned happened on GitHub to the session's item while the
-- session ran, so the sessions panel can say so: the newest of a review of the user's that
-- approved it or requested changes (an agent's gh, a browser, the review sheet), a merge, or
-- a close, '' before any. It is learned from the reads the inbox makes anyway, never a
-- request of its own, and says nothing of who did it. github_event_commit is the commit a
-- review was left on, '' when not known or not a review; github_event_at GitHub's time for
-- it, NULL before any. A later event replaces an earlier one, never the other way round.
CREATE TABLE agent_sessions (
    id                  TEXT     PRIMARY KEY CHECK (id <> ''),
    agent_session_id    TEXT     NOT NULL DEFAULT '',
    resumed_from        TEXT     NOT NULL DEFAULT '',
    agent               TEXT     NOT NULL CHECK (agent <> ''),
    profile             TEXT     NOT NULL DEFAULT '',
    config_dir          TEXT     NOT NULL DEFAULT '',
    prompt              TEXT     NOT NULL DEFAULT '',
    prompt_head         TEXT     NOT NULL DEFAULT '',
    terminal            TEXT     NOT NULL DEFAULT '',
    item_id             INTEGER  NOT NULL DEFAULT 0,
    owner               TEXT     NOT NULL COLLATE NOCASE CHECK (owner <> ''),
    name                TEXT     NOT NULL COLLATE NOCASE CHECK (name <> ''),
    kind                TEXT     NOT NULL CHECK (kind IN ('pr', 'issue')),
    number              INTEGER  NOT NULL CHECK (number > 0),
    title               TEXT     NOT NULL DEFAULT '',
    url                 TEXT     NOT NULL DEFAULT '',
    dir                 TEXT     NOT NULL CHECK (dir <> ''),
    clone_path          TEXT     NOT NULL DEFAULT '',
    worktree            TEXT     NOT NULL DEFAULT '',
    pid                 INTEGER  NOT NULL DEFAULT 0 CHECK (pid >= 0),
    state               TEXT     NOT NULL CHECK (state <> ''),
    state_detail        TEXT     NOT NULL DEFAULT '',
    last_message        TEXT     NOT NULL DEFAULT '',
    launched_at         DATETIME NOT NULL,
    linked_at           DATETIME,
    state_at            DATETIME NOT NULL,
    ended_at            DATETIME,
    permission          TEXT     NOT NULL DEFAULT '',
    worktree_action     TEXT     NOT NULL DEFAULT '',
    launch_notice       TEXT     NOT NULL DEFAULT '',
    run_id              INTEGER  NOT NULL DEFAULT 0 CHECK (run_id >= 0),
    github_event        TEXT     NOT NULL DEFAULT ''
        CHECK (github_event IN ('', 'approved', 'changes_requested', 'merged', 'closed')),
    github_event_commit TEXT     NOT NULL DEFAULT '',
    github_event_at     DATETIME
);

CREATE INDEX agent_sessions_item ON agent_sessions (owner, name, number, launched_at);
CREATE INDEX agent_sessions_state ON agent_sessions (state);

-- == review ==============================================================================

-- The review sheet's cache of each pull request it opened, so the sheet opens at once,
-- offline, and asks GitHub only for what changed since.
--
-- One review_diffs row per pull request the sheet read. head_oid is the head commit its
-- files were listed at; total is how many files the pull request changes, complete
-- whether every one was listed, and bytes the size of the patches kept. context is the
-- JSON of what the sheet read beyond the inbox card (the description, the merge state,
-- what the viewer may do, the checks, and the conversation: its comments, its reviews with
-- their line comments, and its events), its long texts trimmed and marked so one pull
-- request's context stays within 1 MiB, read at context_at; fetched_at is when the files
-- were listed, and used_at when the sheet last opened it. seen_at is GitHub's time of the
-- newest activity in the read the sheet showed when it last opened the pull request, which
-- tells the next visit what is new; NULL before the sheet opened it, or when the read
-- didn't say. Only the sheet's opening moves it: a read leaves it as it was. The review
-- package prunes the rows itself: ones unused for 14 days, the least recently used past
-- 100 rows or 64 MiB of patches, and a pull request's own once it is merged. Items are
-- never deleted, so the foreign key's cascade is only a backstop.
CREATE TABLE review_diffs (
    item_id    INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    head_oid   TEXT     NOT NULL DEFAULT '',
    total      INTEGER  NOT NULL DEFAULT 0 CHECK (total >= 0),
    complete   BOOLEAN  NOT NULL DEFAULT 0 CHECK (complete IN (0, 1)),
    bytes      INTEGER  NOT NULL DEFAULT 0 CHECK (bytes >= 0),
    context    TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(context) AND json_type(context) = 'object'),
    context_at DATETIME,
    fetched_at DATETIME,
    used_at    DATETIME NOT NULL,
    seen_at    DATETIME
);

CREATE INDEX review_diffs_used ON review_diffs (used_at);

-- The files of each cached pull request, in GitHub's order (position). patch is the file's
-- diff as GitHub shows it, and patch_state says why there is none: 'no_content' when GitHub
-- shows no diff (a binary file, or one too large), 'cache_cap' when keeping it would pass
-- the 8 MiB the cache keeps per pull request; '' when patch holds it.
CREATE TABLE review_diff_files (
    item_id       INTEGER NOT NULL REFERENCES review_diffs (item_id) ON DELETE CASCADE,
    position      INTEGER NOT NULL CHECK (position >= 0),
    path          TEXT    NOT NULL CHECK (path <> ''),
    previous_path TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT '',
    additions     INTEGER NOT NULL DEFAULT 0 CHECK (additions >= 0),
    deletions     INTEGER NOT NULL DEFAULT 0 CHECK (deletions >= 0),
    blob_sha      TEXT    NOT NULL DEFAULT '',
    patch         TEXT    NOT NULL DEFAULT '',
    patch_state   TEXT    NOT NULL DEFAULT '' CHECK (patch_state IN ('', 'no_content', 'cache_cap')),
    PRIMARY KEY (item_id, position)
);

-- == issues ==============================================================================

-- The issue sheet's cache of each issue it opened, so the sheet opens at once, offline,
-- and asks GitHub only when the issue may have changed.
--
-- One row per issue the sheet read. view is the JSON of what the sheet read beyond the inbox
-- card (the description, the conversation's newest page, the details beside it, and what the
-- viewer may do), its long texts trimmed and marked so one issue stays within 1 MiB; bytes is
-- its length. fetched_at is when it was read, NULL once an action made it out of date; used_at
-- when the sheet last opened it; seen_at GitHub's time of the newest activity in the read the
-- sheet showed when it last opened the issue, which tells the next visit what is new. The
-- issues package prunes the rows itself: ones unused for 14 days, the least recently used past
-- 200 rows or 32 MiB, and an issue's own once it is closed. Items are never deleted, so the
-- foreign key's cascade is only a backstop.
CREATE TABLE issue_views (
    item_id    INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    view       TEXT     NOT NULL DEFAULT '{}' CHECK (json_valid(view) AND json_type(view) = 'object'),
    bytes      INTEGER  NOT NULL DEFAULT 0 CHECK (bytes >= 0),
    fetched_at DATETIME,
    used_at    DATETIME NOT NULL,
    seen_at    DATETIME
);

CREATE INDEX issue_views_used ON issue_views (used_at);

-- == avatars =============================================================================

-- The profile pictures of the people the sheets show, by login, so each is downloaded once
-- rather than on every look, and shows offline.
--
-- One row per login, lower-cased, as GitHub compares logins. image is the picture's bytes,
-- of the raster type mime names, and bytes its length; fetched_at is when it was
-- downloaded, and used_at when a sheet last showed it, moved at most once a day. A picture
-- downloaded more than seven days ago is downloaded again the next time it is shown. The
-- avatars package prunes the rows itself: ones unused for 30 days, and the least recently
-- used past 2,000 rows or 16 MiB.
CREATE TABLE avatars (
    login      TEXT     PRIMARY KEY CHECK (login <> '' AND login = lower(login)),
    mime       TEXT     NOT NULL CHECK (mime <> ''),
    image      BLOB     NOT NULL,
    bytes      INTEGER  NOT NULL CHECK (bytes >= 0),
    fetched_at DATETIME NOT NULL,
    used_at    DATETIME NOT NULL
);

CREATE INDEX avatars_used ON avatars (used_at);

-- == integrations ========================================================================

-- The tools the app installed or updated from their GitHub releases, one row per install
-- or update. A copy counts as installed by the app while its path and sha256 match the
-- newest row for that path. archive is the release asset it came from, and replaced the
-- version it replaced, or ''.
CREATE TABLE tool_installs (
    id           INTEGER  PRIMARY KEY,
    tool         TEXT     NOT NULL CHECK (tool <> ''),
    version      TEXT     NOT NULL CHECK (version <> ''),
    path         TEXT     NOT NULL CHECK (path <> ''),
    sha256       TEXT     NOT NULL CHECK (length(sha256) = 64),
    archive      TEXT     NOT NULL DEFAULT '',
    replaced     TEXT     NOT NULL DEFAULT '',
    installed_at DATETIME NOT NULL
);

CREATE INDEX tool_installs_path ON tool_installs (path, installed_at);

-- == scriptrun ===========================================================================

-- Every run from the Scripts tab, in the app or in the user's terminal, one row per run.
--
-- kind (release, hooks, checks, script, update, gitPull) and status (running, succeeded,
-- failed, stopped, interrupted, orphaned) are checked in Go, so later kinds need no
-- rebuild. dir is the absolute folder, shown with ~; argv is the JSON argv as started, and
-- command its display form. runs_in is app or terminal. pid and pgid are the process and
-- its group once known, or 0, and proc_started_at is the process's own start time, which
-- tells a live run from a reused pid. result is JSON per kind: a release writes its intent
-- (the expected version, the branch, and the branch the clone was on) at the start, so a
-- run cut short can still be verified. seen_at clears the tab's red dot.
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
