-- 0005_autopilot_routines: the user's Autopilot routines.
--
-- The conventions are 0001_init's: DATETIME stamps the application writes in UTC, BOOLEAN
-- columns kept to 0 or 1, and NOCASE for GitHub logins.

-- == autopilot ===========================================================================

-- One row per routine the user made. spec is the routine's definition, one JSON document
-- with its version (see internal/autopilot/spec), and spec_version that version, as the
-- build that wrote it read it; the name, the description, whether it is on, its mode, and
-- the account it belongs to are its row's, so switching a routine on never touches its
-- definition.
--
-- account_login is the GitHub account that made the routine, '' for one made before any
-- account signed in or brought back from a copy or a backup, which belongs to the account
-- signed in. A routine of another account doesn't run. mode is dryRun, ask, or auto,
-- checked in Go, so a later mode needs no rebuild; a mode this build doesn't know reads
-- as dryRun. template_key names the template a routine was made from, '' for none.
-- sort_order is the routine's place in the list, 0, 1, 2, and so on. enabled_at is when
-- it was last switched on, NULL while it is off. last_error is the last error the routine
-- met, in the window's words, '' for none. broken_reason says why the spec didn't load
-- when the row was last written (newer, unreadable, or invalid, as a routine brought back
-- as it was), '' when it loaded; a read works it out again from the spec, since a newer
-- build may have written it.
--
-- A routine is deleted softly: deleted_at is set, and the row stays, so its history keeps
-- its name and its sentence. Names are unique, ignoring case, among the routines not
-- deleted.
CREATE TABLE autopilot_routines (
    id            INTEGER  PRIMARY KEY,
    account_login TEXT     NOT NULL DEFAULT '' COLLATE NOCASE,
    name          TEXT     NOT NULL COLLATE NOCASE CHECK (name <> ''),
    description   TEXT     NOT NULL DEFAULT '',
    enabled       BOOLEAN  NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    mode          TEXT     NOT NULL CHECK (mode <> ''),
    spec          TEXT     NOT NULL CHECK (spec <> ''),
    spec_version  INTEGER  NOT NULL DEFAULT 0 CHECK (spec_version >= 0),
    template_key  TEXT     NOT NULL DEFAULT '',
    sort_order    INTEGER  NOT NULL DEFAULT 0,
    enabled_at    DATETIME,
    last_error    TEXT     NOT NULL DEFAULT '',
    broken_reason TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    deleted_at    DATETIME,
    CHECK (enabled = 0 OR enabled_at IS NOT NULL),
    CHECK (deleted_at IS NULL OR enabled = 0)
);

CREATE UNIQUE INDEX autopilot_routines_name ON autopilot_routines (name) WHERE deleted_at IS NULL;

CREATE INDEX autopilot_routines_order ON autopilot_routines (sort_order, id) WHERE deleted_at IS NULL;
