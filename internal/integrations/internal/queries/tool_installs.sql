-- name: RecordInstall :exec
INSERT INTO tool_installs (tool, version, path, sha256, archive, replaced, installed_at)
VALUES (sqlc.arg(tool), sqlc.arg(version), sqlc.arg(path), sqlc.arg(sha256), sqlc.arg(archive),
        sqlc.arg(replaced), sqlc.arg(installed_at));

-- name: LatestInstallAt :one
SELECT id, tool, version, path, sha256, archive, replaced, installed_at
FROM tool_installs
WHERE path = sqlc.arg(path)
ORDER BY installed_at DESC, id DESC
LIMIT 1;
