-- 0013_notifications: what the app told the user, newest kept.
--
-- A refresh that finds items newly needing the user records one row per item: a pull
-- request that newly asks for the user's review (review_requested), an issue or pull
-- request newly assigned to them (assigned), or a snoozed item new activity brought back
-- early (woke, with what woke it in detail, such as "approved, 2 new commits"). Each row
-- is kept whether or not a banner was shown, so the window's notification history lists
-- every one. The item's repository, number, title, and URL are copied in, so a row still
-- reads after its item leaves the inbox; item_id links back to it while it is stored.
-- read_at is set once the user has seen the history. Only the newest rows are kept.
CREATE TABLE notifications (
    id         INTEGER  PRIMARY KEY,
    kind       TEXT     NOT NULL CHECK (kind <> ''),
    created_at DATETIME NOT NULL,
    read_at    DATETIME,
    item_id    INTEGER  REFERENCES items (id) ON DELETE SET NULL,
    item_kind  TEXT     NOT NULL DEFAULT '' CHECK (item_kind IN ('', 'pr', 'issue')),
    repo_owner TEXT     NOT NULL DEFAULT '',
    repo_name  TEXT     NOT NULL DEFAULT '',
    number     INTEGER  NOT NULL DEFAULT 0 CHECK (number >= 0),
    title      TEXT     NOT NULL DEFAULT '',
    url        TEXT     NOT NULL DEFAULT '',
    author     TEXT     NOT NULL DEFAULT '',
    detail     TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX notifications_created_at ON notifications (created_at);
