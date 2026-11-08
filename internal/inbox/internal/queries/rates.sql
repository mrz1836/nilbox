-- name: InsertRate :exec
-- InsertRate records one rate-limit reading.
INSERT INTO rate_snapshots (account_id, resource, limit_total, remaining, used, cost, reset_at, taken_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: PruneRates :exec
-- PruneRates removes every rate-limit reading taken before taken_at.
DELETE FROM rate_snapshots WHERE taken_at < ?;

-- name: GetLatestRate :one
-- GetLatestRate returns the newest reading of one budget of the account with account_id,
-- or sql.ErrNoRows when none was taken.
SELECT limit_total, remaining, used, cost, reset_at, taken_at
FROM rate_snapshots
WHERE account_id = ? AND resource = ?
ORDER BY taken_at DESC, id DESC
LIMIT 1;
