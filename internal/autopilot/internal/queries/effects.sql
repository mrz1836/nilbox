-- name: InsertEffect :one
-- InsertEffect records what a match's local step did to its item's triage, in the
-- triage's own transaction, and returns its ID. A step records one effect at most.
INSERT INTO autopilot_effects (
    match_id, step, routine_id, item_id, action, state_before, state_after, applied_at
) VALUES (
    sqlc.arg(match_id), sqlc.arg(step), sqlc.arg(routine_id), sqlc.arg(item_id), sqlc.arg(action),
    sqlc.arg(state_before), sqlc.arg(state_after), sqlc.arg(applied_at)
)
RETURNING id;

-- name: GetStepEffect :one
-- GetStepEffect returns the effect a match's step recorded, or sql.ErrNoRows for none.
SELECT * FROM autopilot_effects WHERE match_id = sqlc.arg(match_id) AND step = sqlc.arg(step);

-- name: ListMatchEffects :many
-- ListMatchEffects returns the effects of the matches among ids, oldest first.
SELECT * FROM autopilot_effects WHERE match_id IN (sqlc.slice(ids)) ORDER BY id;

-- name: ListEffects :many
-- ListEffects returns the effects among ids, newest first: the order Undo puts them back
-- in, so an item two of them changed goes back to the earliest's state before.
SELECT * FROM autopilot_effects WHERE id IN (sqlc.slice(ids)) ORDER BY id DESC;

-- name: SetEffectUndone :execrows
-- SetEffectUndone records that Undo put an effect's item back, while it was neither undone
-- nor reversed.
UPDATE autopilot_effects SET undone_at = sqlc.arg(undone_at)
WHERE id = sqlc.arg(id) AND undone_at IS NULL AND reversed_at IS NULL;

-- name: SetEffectsReversed :execrows
-- SetEffectsReversed records that the user's own triage, at applied_before, took back what
-- a routine did to an item, for the effects among actions neither undone nor reversed yet
-- that it applied by then: a step applied after the user's triage is no part of what the
-- user took back.
UPDATE autopilot_effects SET reversed_at = sqlc.arg(reversed_at)
WHERE routine_id = sqlc.arg(routine_id) AND item_id = sqlc.arg(item_id) AND undone_at IS NULL
  AND reversed_at IS NULL AND applied_at <= sqlc.arg(applied_before) AND action IN (sqlc.slice(actions));

-- name: CountHandsOff :one
-- CountHandsOff returns 1 while a routine leaves an item alone, else 0: what a triage
-- step checks in its own transaction before it records its effect.
SELECT COUNT(*) FROM autopilot_hands_off WHERE routine_id = sqlc.arg(routine_id) AND item_id = sqlc.arg(item_id);

-- name: ListLeftAlone :many
-- ListLeftAlone returns the items the routines leave alone, newest first, each with the
-- item's copy from its routine's latest match on it (NULL with none): Activity's lines
-- that say a routine leaves an item alone, with Let it act again.
SELECT h.routine_id, h.item_id, h.since, h.why, m.repo_owner, m.repo_name, m.kind, m.number, m.title, m.url
FROM autopilot_hands_off h
LEFT JOIN autopilot_matches m ON m.id = (
    SELECT MAX(lm.id) FROM autopilot_matches lm WHERE lm.routine_id = h.routine_id AND lm.item_id = h.item_id
)
ORDER BY h.since DESC, h.routine_id, h.item_id
LIMIT sqlc.arg(row_limit);
