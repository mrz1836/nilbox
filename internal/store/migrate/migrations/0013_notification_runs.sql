-- 0013_notification_runs: the run a notification is about, which a click on it opens.

-- run_id is the script_runs id of a run that failed (run_failed), or 0 for a notification
-- about no run. It is no reference: the run history forgets its oldest runs on its own, and
-- hands an id out again once the history is cleared. run_started is when that run started,
-- as RFC 3339, or '' for none: the window opens the run only while the run with that id
-- started then, so an entry never opens a later run that took its id.
ALTER TABLE notifications ADD COLUMN run_id INTEGER NOT NULL DEFAULT 0 CHECK (run_id >= 0);
ALTER TABLE notifications ADD COLUMN run_started TEXT NOT NULL DEFAULT '';
