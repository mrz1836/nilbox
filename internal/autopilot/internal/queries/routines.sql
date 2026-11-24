-- name: ListRoutines :many
-- ListRoutines returns every routine not deleted, in the user's order.
SELECT id, account_login, name, name_key, description, enabled, mode, spec, spec_version, template_key, sort_order,
       enabled_at, last_error, broken_reason, created_at, updated_at, deleted_at, graduate_declined_at, past_rules_hash,
       uuid
FROM autopilot_routines
WHERE deleted_at IS NULL
ORDER BY sort_order, id;

-- name: GetRoutine :one
-- GetRoutine returns one routine, or sql.ErrNoRows when it is gone or deleted.
SELECT id, account_login, name, name_key, description, enabled, mode, spec, spec_version, template_key, sort_order,
       enabled_at, last_error, broken_reason, created_at, updated_at, deleted_at, graduate_declined_at, past_rules_hash,
       uuid
FROM autopilot_routines
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: CountAllRoutines :one
-- CountAllRoutines counts every routine ever stored, the deleted ones included: none means
-- the table is new.
SELECT COUNT(*) FROM autopilot_routines;

-- name: RoutineNamed :one
-- RoutineNamed returns the ID of the routine not deleted, other than other_than, whose
-- name folds to name_key, or sql.ErrNoRows when there is none.
SELECT id FROM autopilot_routines
WHERE name_key = sqlc.arg(name_key) AND id <> sqlc.arg(other_than) AND deleted_at IS NULL
LIMIT 1;

-- name: InsertRoutine :one
-- InsertRoutine stores a new routine under its UUID, and returns its ID.
INSERT INTO autopilot_routines (
    account_login, name, name_key, description, enabled, mode, spec, spec_version, template_key, sort_order,
    enabled_at, broken_reason, created_at, updated_at, uuid
) VALUES (
    sqlc.arg(account_login), sqlc.arg(name), sqlc.arg(name_key), sqlc.arg(description), sqlc.arg(enabled),
    sqlc.arg(mode), sqlc.arg(spec), sqlc.arg(spec_version), sqlc.arg(template_key), sqlc.arg(sort_order),
    sqlc.narg(enabled_at), sqlc.arg(broken_reason), sqlc.arg(created_at), sqlc.arg(updated_at), sqlc.arg(uuid)
)
RETURNING id;

-- name: UpdateRoutine :execrows
-- UpdateRoutine saves a routine's definition as edited, leaving its place in the order,
-- its template, and its last error, with the hash of the spec its user accepted as one that
-- may merge past the rules, '' for none.
UPDATE autopilot_routines SET
    account_login = sqlc.arg(account_login),
    name = sqlc.arg(name),
    name_key = sqlc.arg(name_key),
    description = sqlc.arg(description),
    enabled = sqlc.arg(enabled),
    mode = sqlc.arg(mode),
    spec = sqlc.arg(spec),
    spec_version = sqlc.arg(spec_version),
    enabled_at = sqlc.narg(enabled_at),
    broken_reason = sqlc.arg(broken_reason),
    past_rules_hash = sqlc.arg(past_rules_hash),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: SetRoutineSpec :execrows
-- SetRoutineSpec replaces a routine's spec alone, and the version it is written in, as when a
-- prompt it names is renamed, while its spec is still the one read (was), with the hash of the
-- spec its user accepted as one that may merge past the rules, '' for none.
UPDATE autopilot_routines SET
    spec = sqlc.arg(spec),
    spec_version = sqlc.arg(spec_version),
    past_rules_hash = sqlc.arg(past_rules_hash),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND spec = sqlc.arg(was) AND deleted_at IS NULL;

-- name: SetRoutinePastRules :execrows
-- SetRoutinePastRules records the hash of the spec a new routine's user accepted as one
-- that may merge past the rules, '' for none.
UPDATE autopilot_routines SET past_rules_hash = sqlc.arg(past_rules_hash)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: SetRoutineEnabled :execrows
-- SetRoutineEnabled switches a routine on, with when, or off, with no time.
UPDATE autopilot_routines SET
    enabled = sqlc.arg(enabled),
    enabled_at = sqlc.narg(enabled_at),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: SetRoutineMode :execrows
-- SetRoutineMode sets a routine's mode.
UPDATE autopilot_routines SET
    mode = sqlc.arg(mode),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: SetRoutineOrder :exec
-- SetRoutineOrder puts a routine at a place in the order.
UPDATE autopilot_routines SET sort_order = sqlc.arg(sort_order) WHERE id = sqlc.arg(id);

-- name: DeleteRoutine :execrows
-- DeleteRoutine deletes a routine softly: it is switched off and marked deleted, and its
-- row stays for the history that names it.
UPDATE autopilot_routines SET
    enabled = 0,
    enabled_at = NULL,
    deleted_at = sqlc.arg(deleted_at),
    updated_at = sqlc.arg(deleted_at)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: SetRoutineGraduateDeclined :execrows
-- SetRoutineGraduateDeclined records when the user said Not now to putting a routine on
-- Autopilot.
UPDATE autopilot_routines SET
    graduate_declined_at = sqlc.arg(declined_at),
    updated_at = sqlc.arg(declined_at)
WHERE id = sqlc.arg(id) AND deleted_at IS NULL;

-- name: CountRoutineUUID :one
-- CountRoutineUUID counts the routines with uuid, the deleted ones included, whose UUIDs
-- are never given again: one means it is taken.
SELECT COUNT(*) FROM autopilot_routines WHERE uuid = sqlc.arg(uuid);

-- name: RoutinesWithoutUUID :many
-- RoutinesWithoutUUID returns the IDs of the routines with no UUID yet, the deleted ones
-- included, as a build from before UUIDs inserts them.
SELECT id FROM autopilot_routines WHERE uuid = '' ORDER BY id;

-- name: SetRoutineUUID :exec
-- SetRoutineUUID gives a routine a UUID. Nothing else changes, its updated_at included.
UPDATE autopilot_routines SET uuid = sqlc.arg(uuid) WHERE id = sqlc.arg(id);
