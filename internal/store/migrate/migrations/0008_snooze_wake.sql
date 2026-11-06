-- 0008_snooze_wake: what brings a snoozed item back early.
--
-- head_oid is a pull request's head commit SHA, read with its details on every refresh; a
-- new one means it was pushed, force-pushes included. snooze_baseline is a JSON object of
-- the item's activity when it was snoozed (its head, commit and comment counts, and
-- reviews), which each refresh compares with the item's details under the user's wake
-- rules (the snooze.* prefs). wake_reasons is a JSON object of the new activity that ended
-- a snooze early; it is NULL while a snooze runs and when one ran its course.
ALTER TABLE item_details ADD COLUMN head_oid TEXT NOT NULL DEFAULT '';

ALTER TABLE item_local_state ADD COLUMN snooze_baseline TEXT
    CHECK (snooze_baseline IS NULL OR (json_valid(snooze_baseline) AND json_type(snooze_baseline) = 'object'));

ALTER TABLE item_local_state ADD COLUMN wake_reasons TEXT
    CHECK (wake_reasons IS NULL OR (json_valid(wake_reasons) AND json_type(wake_reasons) = 'object'));
