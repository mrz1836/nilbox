-- 0009_pull_reviews: the review sheet's cache of each pull request it opened, so the sheet
-- opens at once, offline, and asks GitHub only for what changed since.
--
-- One review_diffs row per pull request the sheet read. head_oid is the head commit its
-- files were listed at; total is how many files the pull request changes, complete
-- whether every one was listed, and bytes the size of the patches kept. context is the
-- JSON of what the sheet read beyond the inbox card (the description, the merge state,
-- what the viewer may do, the reviews, and the checks), read at context_at; fetched_at
-- is when the files were listed, and used_at when the sheet last opened it. The review
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
    used_at    DATETIME NOT NULL
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
