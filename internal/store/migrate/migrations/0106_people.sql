-- 0106_people: who works on each repository, for the review sheet's reviewer picker.
--
-- The conventions are 0100_settings's. The people package owns every table here, and prunes
-- them itself, in the same transaction that writes them.

-- == people ==============================================================================

-- The GitHub accounts the app learned of as people (bots and mannequins are never kept),
-- one row per login: login_key is the login lower-cased, as GitHub compares logins, login
-- as GitHub spells it, and name the profile's display name, '' when none was read. A row no
-- people_seen row names any longer is dropped.
CREATE TABLE people (
    login_key  TEXT     PRIMARY KEY CHECK (login_key <> '' AND login_key = lower(login_key)),
    login      TEXT     NOT NULL CHECK (login <> ''),
    name       TEXT     NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL
);

-- What the app saw each person do on a repository's pull requests: one row per repository
-- ("owner/name"), person, pull request number, and role, so reading one pull request again
-- never counts anything twice. role is one of:
--   author     opened the pull request
--   reviewer   left a review on it
--   committer  authored one of its commits
--   requested  was asked to review it
--   suggested  was suggested by GitHub as a reviewer of it
--   picked     was asked to review it by the user, from the app
-- access reports an association GitHub gave as OWNER, MEMBER, or COLLABORATOR on the pull
-- request they opened or the review they left, and at is GitHub's time of what was seen
-- (the review's submission, the commit, or the pull request's last update), or the app's
-- for a pick. The package keeps, per repository, the rows of the newest 400 pull request
-- numbers, rows a year old at most, and 100,000 rows in all.
CREATE TABLE people_seen (
    repo      TEXT     NOT NULL COLLATE NOCASE CHECK (repo <> ''),
    login_key TEXT     NOT NULL REFERENCES people (login_key) ON DELETE CASCADE,
    number    INTEGER  NOT NULL CHECK (number > 0),
    role      TEXT     NOT NULL CHECK (role IN ('author', 'reviewer', 'committer', 'requested', 'suggested', 'picked')),
    access    BOOLEAN  NOT NULL DEFAULT 0 CHECK (access IN (0, 1)),
    at        DATETIME NOT NULL,
    PRIMARY KEY (repo, login_key, number, role)
);

CREATE INDEX people_seen_login ON people_seen (login_key);
CREATE INDEX people_seen_at ON people_seen (at);

-- A repository's teams, as its teams list named them (listed) or as a pull request asked
-- one to review (requested_at, the app's time it was last seen asked, NULL for never):
-- slug_key is the slug lower-cased, slug as GitHub spells it, and name the team's display
-- name, '' when none was read. A team neither listed nor asked for in a year is dropped.
CREATE TABLE people_teams (
    repo         TEXT     NOT NULL COLLATE NOCASE CHECK (repo <> ''),
    slug_key     TEXT     NOT NULL CHECK (slug_key <> '' AND slug_key = lower(slug_key)),
    slug         TEXT     NOT NULL CHECK (slug <> ''),
    name         TEXT     NOT NULL DEFAULT '',
    listed       BOOLEAN  NOT NULL DEFAULT 0 CHECK (listed IN (0, 1)),
    requested_at DATETIME,
    PRIMARY KEY (repo, slug_key)
);

-- When the app last read a repository's recent pull requests for who works on it: read_at
-- when a read last went through, NULL before one did, with pulls how many pull requests it
-- read and org whether an organization owns the repository; failed_at when the last read
-- failed, NULL when none did since. A row neither read nor failed in a year is dropped.
CREATE TABLE people_reads (
    repo      TEXT     PRIMARY KEY COLLATE NOCASE CHECK (repo <> ''),
    read_at   DATETIME,
    failed_at DATETIME,
    pulls     INTEGER  NOT NULL DEFAULT 0 CHECK (pulls >= 0),
    org       BOOLEAN  NOT NULL DEFAULT 0 CHECK (org IN (0, 1))
);
