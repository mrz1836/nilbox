-- 0017_avatars: the profile pictures of the people the sheets show, by login, so each is
-- downloaded once rather than on every look, and shows offline.
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
