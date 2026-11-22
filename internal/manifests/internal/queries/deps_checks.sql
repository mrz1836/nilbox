-- name: ListChecks :many
-- ListChecks returns every repository the user marked up to date, with when they did.
SELECT repo_owner, repo_name, checked_at
FROM deps_checks;

-- name: SetCheck :exec
-- SetCheck records that the user marked a repository up to date, in place of when they
-- last did.
INSERT INTO deps_checks (repo_owner, repo_name, checked_at)
VALUES (sqlc.arg(repo_owner), sqlc.arg(repo_name), sqlc.arg(checked_at))
ON CONFLICT (repo_owner, repo_name) DO UPDATE SET
    checked_at = excluded.checked_at;

-- name: ClearCheck :exec
-- ClearCheck forgets that the user marked a repository up to date.
DELETE FROM deps_checks
WHERE repo_owner = sqlc.arg(repo_owner) AND repo_name = sqlc.arg(repo_name);
