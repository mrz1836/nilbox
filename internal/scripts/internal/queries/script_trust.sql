-- name: RepoTrust :many
-- RepoTrust returns every command the user trusted in a repository, for the page.
SELECT source, name, sha256, command, granted_at
FROM script_trust
WHERE repo_owner = sqlc.arg(repo_owner) AND repo_name = sqlc.arg(repo_name);

-- name: GetTrust :one
-- GetTrust returns the command the user trusted under a repository, a source, and a name,
-- or sql.ErrNoRows when they never did.
SELECT sha256, command, granted_at
FROM script_trust
WHERE repo_owner = sqlc.arg(repo_owner) AND repo_name = sqlc.arg(repo_name)
    AND source = sqlc.arg(source) AND name = sqlc.arg(name);

-- name: SetTrust :exec
-- SetTrust records that the user trusted a command, in place of what they trusted before
-- under the same repository, source, and name.
INSERT INTO script_trust (repo_owner, repo_name, source, name, sha256, command, granted_at)
VALUES (sqlc.arg(repo_owner), sqlc.arg(repo_name), sqlc.arg(source), sqlc.arg(name), sqlc.arg(sha256),
        sqlc.arg(command), sqlc.arg(granted_at))
ON CONFLICT (repo_owner, repo_name, source, name) DO UPDATE SET
    sha256 = excluded.sha256,
    command = excluded.command,
    granted_at = excluded.granted_at;
