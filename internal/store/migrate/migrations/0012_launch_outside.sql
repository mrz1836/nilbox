-- 0012_launch_outside: whether Autopilot may run agents on outside contributors' items, per
-- org or repository.
--
-- autopilot_outside is NULL to inherit (from the repository's org, else off: there is no
-- app-wide switch), 'off', or 'on'. On still means plan mode: an automatic run never lifts
-- the outside contributor restriction, only a person approving one item does. A
-- repository's choice wins over its org's, as the other launch defaults do. The launch
-- defaults own the column.

-- == launchdefaults ======================================================================

ALTER TABLE launch_defaults ADD COLUMN autopilot_outside TEXT CHECK (autopilot_outside IN ('off', 'on'));
