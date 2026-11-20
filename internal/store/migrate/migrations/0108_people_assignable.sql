-- 0108_people_assignable: who can be assigned in each repository, for both sheets' assignee
-- pickers.
--
-- The conventions are 0100_settings's. The people package owns every table here, and prunes
-- them itself, in the same transaction that writes them. A person here is a row of people
-- (0106_people), which is one person across every repository, so what the app learns of them
-- in one repository serves the pickers of every other.

-- == people ==============================================================================

-- Who the app learned can be assigned to a repository's issues and pull requests, one row
-- per repository ("owner/name") and person: listed_at is when a read of GitHub's assignable
-- users of the repository last named them, the whole list or a search, NULL when none did,
-- or when a read of the whole list that named everyone left them out since; assigned_at is
-- when the app last saw them assigned to one of its issues or pull requests, on a read or by
-- the user's own edit, NULL for never. A row whose two times are both NULL or a year old is
-- dropped, and so is the oldest past 100,000 rows in all.
CREATE TABLE people_assignable (
    repo        TEXT     NOT NULL COLLATE NOCASE CHECK (repo <> ''),
    login_key   TEXT     NOT NULL REFERENCES people (login_key) ON DELETE CASCADE,
    listed_at   DATETIME,
    assigned_at DATETIME,
    PRIMARY KEY (repo, login_key)
);

CREATE INDEX people_assignable_login ON people_assignable (login_key);

-- The reads of a repository's assignable users, one row per repository and search:
-- query_key is the search's words, trimmed and lower-cased, and '' for the whole list (one
-- page of 100). read_at is when the last read went through, NULL before one did, with total
-- how many people GitHub said match and listed how many it named, at most 100, so a read
-- with listed >= total named everyone; failed_at is when the last read failed, NULL when none
-- did since. A search's row is dropped a week after its last read, when it no longer covers
-- a longer search, and the whole list's a year after.
CREATE TABLE people_assignable_reads (
    repo      TEXT     NOT NULL COLLATE NOCASE CHECK (repo <> ''),
    query_key TEXT     NOT NULL CHECK (query_key = lower(trim(query_key))),
    read_at   DATETIME,
    failed_at DATETIME,
    total     INTEGER  NOT NULL DEFAULT 0 CHECK (total >= 0),
    listed    INTEGER  NOT NULL DEFAULT 0 CHECK (listed >= 0),
    PRIMARY KEY (repo, query_key)
);
