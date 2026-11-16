-- 0008_notification_targets: what an Autopilot notification is headed by and opens.
--
-- The conventions are 0001_init's: DATETIME stamps the application writes in UTC. The
-- notify package owns every column here.

-- == notifications =======================================================================

-- heading heads the entry and its banner in place of its kind's title, as an Autopilot
-- alert is headed by its routine's name; '' for the kind's own title.
--
-- autopilot_routine, autopilot_match, and autopilot_run are what an Autopilot notification
-- opens in the Autopilot tab: its routine, and the match (its card in Activity) or the
-- agent run (the run view) it names, each 0 for none. autopilot_created is when the most
-- specific of them was created (the run's, else the match's, else the routine's), as RFC
-- 3339, or '': the window opens it only while the one with that ID was created then, since
-- Clear history hands an ID out again.
ALTER TABLE notifications ADD COLUMN heading TEXT NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN autopilot_routine INTEGER NOT NULL DEFAULT 0 CHECK (autopilot_routine >= 0);
ALTER TABLE notifications ADD COLUMN autopilot_match INTEGER NOT NULL DEFAULT 0 CHECK (autopilot_match >= 0);
ALTER TABLE notifications ADD COLUMN autopilot_run INTEGER NOT NULL DEFAULT 0 CHECK (autopilot_run >= 0);
ALTER TABLE notifications ADD COLUMN autopilot_created TEXT NOT NULL DEFAULT '';

-- once_key tells a notice from every other, so the log keeps it once while it holds it: an
-- Autopilot step's notice is keyed by its match, when the match was made, the step, and
-- the try, and a problem by its routine, its kind, and when it began, so a step run again
-- after a quit, or a problem told again after a crash, adds no second entry and no second
-- banner. '' for every other notification, which is never refused.
ALTER TABLE notifications ADD COLUMN once_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX notifications_once_key ON notifications (once_key) WHERE once_key <> '';
