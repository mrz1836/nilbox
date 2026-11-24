-- 0116_record_uuids: a stable UUID for every prompt, user script, and routine.
--
-- The conventions are 0100_settings's. The prompts package owns prompts, the scripts
-- package user_scripts, and the autopilot package autopilot_routines; this migration only
-- adds a column to each and fills it in.

-- A routine names the prompts and the scripts its steps run, and a backup names its
-- routines. A nickname, a name, or a row's id each changes or comes back different: a prompt
-- is renamed, reset, or imported over, a script's id is reused or renumbered by a restore,
-- and a routine is renamed. So each of the three tables carries a uuid that never changes
-- once given: a reference names a record by it first, and by its old name or id only as a
-- fallback.
--
-- uuid is a random version 4 UUID in its canonical form, 36 lowercase characters. The
-- column is added as '' for every row, then each row is given its own here, inside this
-- migration's transaction, after the backup store.Open takes before migrating, so no row
-- is left without one. A build from before this one still running inserts '', which the
-- owner gives a UUID at its next start; the unique index leaves '' out, so any number of
-- those rows may wait. A routine deleted softly keeps its uuid, so a uuid is never given
-- twice.
--
-- hex(randomblob(n)) is 2n random hex digits. The third group starts with 4, the version,
-- and the fourth with 8, 9, a, or b, the RFC 9562 variant, chosen by random()'s low two
-- bits; & 3 keeps those whatever random()'s sign.

-- == prompts =============================================================================

ALTER TABLE prompts ADD COLUMN uuid TEXT NOT NULL DEFAULT '';

UPDATE prompts SET uuid = lower(
    hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-'
    || substr('89ab', 1 + (random() & 3), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))
) WHERE uuid = '';

CREATE UNIQUE INDEX prompts_uuid ON prompts (uuid) WHERE uuid <> '';

-- == user_scripts ========================================================================

ALTER TABLE user_scripts ADD COLUMN uuid TEXT NOT NULL DEFAULT '';

UPDATE user_scripts SET uuid = lower(
    hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-'
    || substr('89ab', 1 + (random() & 3), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))
) WHERE uuid = '';

CREATE UNIQUE INDEX user_scripts_uuid ON user_scripts (uuid) WHERE uuid <> '';

-- == autopilot_routines ==================================================================

ALTER TABLE autopilot_routines ADD COLUMN uuid TEXT NOT NULL DEFAULT '';

UPDATE autopilot_routines SET uuid = lower(
    hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-'
    || substr('89ab', 1 + (random() & 3), 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6))
) WHERE uuid = '';

CREATE UNIQUE INDEX autopilot_routines_uuid ON autopilot_routines (uuid) WHERE uuid <> '';
