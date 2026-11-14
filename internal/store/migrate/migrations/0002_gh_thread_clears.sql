-- 0002_gh_thread_clears: clearing the user's GitHub notifications, by their rules.
--
-- The conventions are 0001_init's: DATETIME stamps the application writes in UTC, BOOLEAN
-- columns kept to 0 or 1, and NOCASE for GitHub logins and repository names.

-- == ghthreads ===========================================================================

-- The moments the user finished an item in the app, which clear its notification threads
-- by the rules: a merge (merged), a close (pr_closed or issue_closed), a review that
-- approves or requests changes (reviewed), a dismissal (dismissed), or taking themselves
-- off it (unassigned), one row per account, item, and trigger, the latest kept. at is the
-- moment itself (the click, or the review's own time), and created_at when the app heard
-- of it, which the Undo hold runs from. A moment from GitHub's own state (a merge or a
-- close elsewhere) is never stored here: it is read from gh_threads. trigger is checked
-- in Go, so a later trigger needs no rebuild. The ghthreads package drops a moment 30 days
-- after it was heard of.
CREATE TABLE gh_thread_moments (
    account_login TEXT     NOT NULL COLLATE NOCASE CHECK (account_login <> ''),
    repo_owner    TEXT     NOT NULL COLLATE NOCASE CHECK (repo_owner <> ''),
    repo_name     TEXT     NOT NULL COLLATE NOCASE CHECK (repo_name <> ''),
    item_kind     TEXT     NOT NULL CHECK (item_kind IN ('pr', 'issue')),
    number        INTEGER  NOT NULL CHECK (number > 0),
    trigger       TEXT     NOT NULL CHECK (trigger <> '' AND trigger <> 'by_hand'),
    at            DATETIME NOT NULL,
    created_at    DATETIME NOT NULL,
    PRIMARY KEY (account_login, repo_owner, repo_name, item_kind, number, trigger)
);

-- The ledger of clearing: one row per clear the app decided on, written before anything is
-- sent, so every clear sent has a row, with the rule that chose it.
--
-- account_login and thread_id name the thread; repo_owner, repo_name, item_kind, number,
-- title, and reason are copied from it, so a row reads after its thread is gone. trigger
-- is the moment that chose it (by_hand for the user's own click), checked in Go, and action
-- what clearing does: done (DELETE) or read (PATCH). thread_updated_at is the thread's
-- updated_at the clear was decided on, which the thread must still have when it is sent.
--
-- state is queued until the clear settles: sent (GitHub took it, or no longer had the
-- thread), kept (the user undid it), failed (three attempts failed), or skipped (no
-- longer cleared when it was to be sent, or clearing turned off; skip_reason says why).
-- queued_at is when it was decided, send_after when it may go (the Undo hold's end, or at
-- once), sent_at when GitHub took it, and settled_at when it left the queue. attempts
-- counts the sends that failed, and error is the last failure in the window's words.
--
-- At most one row per account and thread is queued. The ghthreads package drops a row 30
-- days after it settled.
CREATE TABLE gh_thread_clears (
    id                INTEGER  PRIMARY KEY,
    account_login     TEXT     NOT NULL COLLATE NOCASE CHECK (account_login <> ''),
    thread_id         INTEGER  NOT NULL CHECK (thread_id > 0),
    repo_owner        TEXT     NOT NULL COLLATE NOCASE CHECK (repo_owner <> ''),
    repo_name         TEXT     NOT NULL COLLATE NOCASE CHECK (repo_name <> ''),
    item_kind         TEXT     NOT NULL CHECK (item_kind IN ('pr', 'issue')),
    number            INTEGER  NOT NULL CHECK (number > 0),
    title             TEXT     NOT NULL DEFAULT '',
    reason            TEXT     NOT NULL DEFAULT '',
    trigger           TEXT     NOT NULL CHECK (trigger <> ''),
    action            TEXT     NOT NULL CHECK (action IN ('done', 'read')),
    state             TEXT     NOT NULL CHECK (state IN ('queued', 'sent', 'kept', 'failed', 'skipped')),
    thread_updated_at DATETIME NOT NULL,
    queued_at         DATETIME NOT NULL,
    send_after        DATETIME NOT NULL,
    sent_at           DATETIME,
    settled_at        DATETIME,
    attempts          INTEGER  NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    error             TEXT     NOT NULL DEFAULT '',
    skip_reason       TEXT     NOT NULL DEFAULT '',
    CHECK ((state = 'queued') = (settled_at IS NULL)),
    CHECK ((state = 'sent') = (sent_at IS NOT NULL))
);

CREATE INDEX gh_thread_clears_due ON gh_thread_clears (state, send_after);

CREATE UNIQUE INDEX gh_thread_clears_queued ON gh_thread_clears (account_login, thread_id) WHERE state = 'queued';

CREATE INDEX gh_thread_clears_account ON gh_thread_clears (account_login, thread_id);

CREATE INDEX gh_thread_clears_settled ON gh_thread_clears (settled_at);
