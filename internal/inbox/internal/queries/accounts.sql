-- name: UpsertAccount :one
-- UpsertAccount stores the signed-in account and returns its id. An account already
-- stored under the same login, which matches ignoring case since the column is declared
-- COLLATE NOCASE, takes the login's casing as given and has updated_at stamped; its
-- created_at stays as first stored.
INSERT INTO accounts (login, created_at, updated_at) VALUES (?, ?, ?)
ON CONFLICT (login) DO UPDATE SET login = excluded.login, updated_at = excluded.updated_at
RETURNING id;

-- name: AccountID :one
-- AccountID returns the id of the account with login, or sql.ErrNoRows when none is
-- stored.
SELECT id FROM accounts WHERE login = ?;
