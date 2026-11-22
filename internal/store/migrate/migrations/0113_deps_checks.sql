-- 0113_deps_checks: the repositories the user marked up to date on the Deps page.
--
-- The conventions are 0100_settings's. The manifests package owns deps_checks.

-- == manifests ===========================================================================

-- A repository whose dependencies the user marked up to date on the Deps page, one row
-- each, all of its clones sharing it, a triage of the user's own, as snoozing an item is:
-- checked_at is when they last did, which a clone's age counts from while its stalest
-- go.mod or package.json changed before then. Unmarking it deletes the row.
CREATE TABLE deps_checks (
    repo_owner TEXT     NOT NULL COLLATE NOCASE CHECK (repo_owner <> ''),
    repo_name  TEXT     NOT NULL COLLATE NOCASE CHECK (repo_name <> ''),
    checked_at DATETIME NOT NULL,
    PRIMARY KEY (repo_owner, repo_name)
);
