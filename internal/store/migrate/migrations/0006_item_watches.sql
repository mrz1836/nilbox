-- 0006_item_watches: the items the routines' searches watch, the checks read for them,
-- and what every read says of an item's state.
--
-- The conventions are 0001_init's: DATETIME stamps the application writes in UTC, and
-- BOOLEAN columns kept to 0 or 1. The inbox owns every table and column here.

-- == inbox ===============================================================================

-- state is the item's state as the last read that carries it said: OPEN, CLOSED, or MERGED
-- (a pull request's only), '' before a read said; closed_at is when it was merged or
-- closed, NULL while it is open or when the read didn't say. The inbox query's searches,
-- an item's own read, the checks follower's, a routine's search, and the states other
-- services read all write them.
--
-- review_requested_at and assigned_at are when the inbox first saw the item in Review
-- requested and in Assigned, while it is there; each is NULL once the item leaves that
-- list, so the next arrival stamps it again.
ALTER TABLE items ADD COLUMN state TEXT NOT NULL DEFAULT '';
ALTER TABLE items ADD COLUMN closed_at DATETIME;
ALTER TABLE items ADD COLUMN review_requested_at DATETIME;
ALTER TABLE items ADD COLUMN assigned_at DATETIME;

-- base_ref is the branch a pull request merges into, and merge_state GitHub's
-- mergeStateStatus for it (BEHIND, BLOCKED, CLEAN, DIRTY, HAS_HOOKS, UNKNOWN, UNSTABLE);
-- both '' for an issue and before a read brought them.
ALTER TABLE item_details ADD COLUMN base_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE item_details ADD COLUMN merge_state TEXT NOT NULL DEFAULT '';

-- One row per routine search and the item it returned. watch_key is the search's query, as
-- the routines' builder writes it in one canonical order, so identical searches share it.
-- follow says the checks follower reads the item while its checks run, as it reads a
-- listed pull request, since a routine on it acts on its checks. seen_at is when the
-- search last returned the item. stale is set when the search failed, or returned only its
-- 100 most recently updated items and this one wasn't among them: the row is kept, so the
-- item doesn't look as if it left the watch, until the search returns without it. A row
-- whose search no routine has any more is deleted.
CREATE TABLE item_watches (
    watch_key TEXT     NOT NULL CHECK (watch_key <> ''),
    item_id   INTEGER  NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    follow    BOOLEAN  NOT NULL DEFAULT 0 CHECK (follow IN (0, 1)),
    seen_at   DATETIME NOT NULL,
    stale     BOOLEAN  NOT NULL DEFAULT 0 CHECK (stale IN (0, 1)),
    PRIMARY KEY (watch_key, item_id)
);

CREATE INDEX item_watches_item_id ON item_watches (item_id);

-- A pull request's checks on one head commit, as read: checks is a JSON array of objects,
-- one per check run or commit status, with its name, workflow ('' for an app's own run and
-- for a status), app, status, conclusion, outcome (passed, failed, pending, or skipped),
-- URL, whether it is required (null when not read), and when it started and completed
-- (null while it runs). A name may repeat, once per event that ran it. complete says checks
-- holds every check of the commit; settled_at is the newest completion, once every check
-- has finished, NULL while one runs. The row is replaced on each read, and goes with its
-- item.
CREATE TABLE item_checks (
    item_id    INTEGER  PRIMARY KEY REFERENCES items (id) ON DELETE CASCADE,
    head_oid   TEXT     NOT NULL DEFAULT '',
    checks     TEXT     NOT NULL DEFAULT '[]' CHECK (json_valid(checks) AND json_type(checks) = 'array'),
    complete   BOOLEAN  NOT NULL DEFAULT 0 CHECK (complete IN (0, 1)),
    settled_at DATETIME,
    fetched_at DATETIME NOT NULL
);
