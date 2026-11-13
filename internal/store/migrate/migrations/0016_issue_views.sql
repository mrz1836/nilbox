-- 0016_issue_views: the issue sheet's cache of each issue it opened, so the sheet opens at
-- once, offline, and asks GitHub only when the issue may have changed.
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
