-- name: UpsertPerson :exec
-- UpsertPerson keeps the person login_key names, with the login as GitHub spells it now and
-- their name, keeping the name kept when the new one is empty.
INSERT INTO people (login_key, login, name, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (login_key) DO UPDATE SET
    login = excluded.login,
    name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE people.name END,
    updated_at = excluded.updated_at;

-- name: UpsertSeen :exec
-- UpsertSeen keeps what a person was seen doing on one pull request of a repository: a row
-- seen before keeps the access either saw, and the later time.
INSERT INTO people_seen (repo, login_key, number, role, access, at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (repo, login_key, number, role) DO UPDATE SET
    access = CASE WHEN excluded.access THEN 1 ELSE people_seen.access END,
    at = CASE WHEN excluded.at > people_seen.at THEN excluded.at ELSE people_seen.at END;

-- name: ListSeen :many
-- ListSeen returns everything seen on the pull requests of a repository, with each person's
-- login and name.
SELECT s.login_key, p.login, p.name, s.number, s.role, s.access, s.at
FROM people_seen s
JOIN people p ON p.login_key = s.login_key
WHERE s.repo = ?;

-- name: PruneSeenOfRepo :execrows
-- PruneSeenOfRepo drops what was seen on a repository's pull requests older than its newest
-- row_limit pull request numbers.
DELETE FROM people_seen
WHERE people_seen.repo = sqlc.arg(repo) AND people_seen.number NOT IN (
    SELECT DISTINCT kept.number FROM people_seen kept
    WHERE kept.repo = sqlc.arg(repo)
    ORDER BY kept.number DESC
    LIMIT sqlc.arg(row_limit)
);

-- name: PruneSeenBefore :execrows
-- PruneSeenBefore drops everything seen before a time.
DELETE FROM people_seen WHERE at < ?;

-- name: PruneSeenPast :execrows
-- PruneSeenPast drops the rows seen earliest past the newest row_limit rows in all.
DELETE FROM people_seen
WHERE rowid IN (
    SELECT oldest.rowid FROM people_seen oldest
    ORDER BY oldest.at, oldest.rowid
    LIMIT max(0, (SELECT COUNT(*) FROM people_seen) - CAST(sqlc.arg(row_limit) AS INTEGER))
);

-- name: PruneUnseenPeople :execrows
-- PruneUnseenPeople drops the people nothing seen names any longer.
DELETE FROM people WHERE login_key NOT IN (SELECT seen.login_key FROM people_seen seen);

-- name: UnlistTeams :exec
-- UnlistTeams marks every team of a repository as not on its teams list, before the list
-- just read marks its own again.
UPDATE people_teams SET listed = 0 WHERE repo = ?;

-- name: UpsertListedTeam :exec
-- UpsertListedTeam keeps a team the repository's teams list names, with its name, keeping
-- the name kept when the new one is empty.
INSERT INTO people_teams (repo, slug_key, slug, name, listed)
VALUES (?, ?, ?, ?, 1)
ON CONFLICT (repo, slug_key) DO UPDATE SET
    slug = excluded.slug,
    name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE people_teams.name END,
    listed = 1;

-- name: UpsertRequestedTeam :exec
-- UpsertRequestedTeam keeps a team a pull request of the repository asked to review, at
-- requested_at, with its name, keeping the name kept when the new one is empty.
INSERT INTO people_teams (repo, slug_key, slug, name, requested_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (repo, slug_key) DO UPDATE SET
    slug = excluded.slug,
    name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE people_teams.name END,
    requested_at = excluded.requested_at;

-- name: ListTeams :many
-- ListTeams returns a repository's teams, the most recently asked to review first, then the
-- rest by slug.
SELECT slug, name, listed, requested_at
FROM people_teams
WHERE repo = ?
ORDER BY requested_at DESC, slug_key;

-- name: PruneTeams :execrows
-- PruneTeams drops the teams no teams list names that no pull request asked for since a
-- time.
DELETE FROM people_teams WHERE listed = 0 AND (requested_at IS NULL OR requested_at < ?);

-- name: MarkRead :exec
-- MarkRead records a read of a repository's recent pull requests that went through, which
-- clears a failure.
INSERT INTO people_reads (repo, read_at, failed_at, pulls, org)
VALUES (?, ?, NULL, ?, ?)
ON CONFLICT (repo) DO UPDATE SET
    read_at = excluded.read_at,
    failed_at = NULL,
    pulls = excluded.pulls,
    org = excluded.org;

-- name: MarkFailed :exec
-- MarkFailed records a read of a repository's recent pull requests that failed.
INSERT INTO people_reads (repo, failed_at)
VALUES (?, ?)
ON CONFLICT (repo) DO UPDATE SET failed_at = excluded.failed_at;

-- name: GetRead :one
-- GetRead returns when a repository's recent pull requests were last read, and failed to be.
SELECT read_at, failed_at, pulls, org FROM people_reads WHERE repo = ?;

-- name: PruneReads :execrows
-- PruneReads drops the reads of the repositories neither read nor failed since a time.
DELETE FROM people_reads
WHERE (read_at IS NULL OR read_at < sqlc.arg(before))
    AND (failed_at IS NULL OR failed_at < sqlc.arg(before));
