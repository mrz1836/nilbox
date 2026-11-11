-- 0013_notification_runs: the run a notification is about, which a click on it opens.

-- run_id is the script_runs id of a run that failed (run_failed), or 0 for a notification
-- about no run. It is no reference: the run history forgets its oldest runs on its own,
-- and the notification keeps naming the run, which the window then can't find.
ALTER TABLE notifications ADD COLUMN run_id INTEGER NOT NULL DEFAULT 0 CHECK (run_id >= 0);
