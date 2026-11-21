-- 0110_routine_past_rules: the routine its user accepted as one that may merge their own
-- pull requests past the rules.
--
-- The conventions are 0100_settings's. The autopilot package owns every column here.

-- == autopilot_routines ==================================================================

-- A routine whose Merge it may go past a branch's rules (its allowBypass or allowSensitive)
-- merges so only once the user accepted it as such, when they saved it, and only while
-- Autopilot's switch for it is on. past_rules_hash is the full SHA-256, in lowercase hex, of
-- the spec text they accepted, '' for none: it stands only while the stored spec is that
-- text, so any rewrite of the spec leaves it standing for nothing. Only Save, with the
-- user's acceptance, sets it; Save without a tick, and a backup's import, clear it; the
-- prompt library's rename of a prompt carries it to the renamed spec only while it stood;
-- and a copy (Duplicate, Restore, Import) starts with none. No export, backup, or
-- autopilot.json ever holds it.
ALTER TABLE autopilot_routines ADD COLUMN past_rules_hash TEXT NOT NULL DEFAULT '';
