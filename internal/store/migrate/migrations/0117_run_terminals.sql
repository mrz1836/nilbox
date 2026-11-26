-- 0117_run_terminals: the terminal app a run in the terminal opened in.
--
-- The conventions are 0100_settings's. The scriptrun package owns script_runs.

-- == scriptrun ===========================================================================

-- A run in the user's terminal opens in the terminal chosen in Settings → Launching as it
-- starts, and the user may choose another while it goes, or long after. So the run records
-- the one it opened in, and the window names that one ("It runs in iTerm2"), and Show in
-- terminal brings the run's tab forward there, whatever is chosen now.
--
-- terminal is that terminal's settings ID (iterm2, warp, or terminal), checked in Go, as the
-- launch setting is, so a later terminal needs no rebuild. It is '' for a run in the app, a
-- run whose terminal never opened, and every run recorded before this, which the window
-- names by the terminal chosen now, as it did.
ALTER TABLE script_runs ADD COLUMN terminal TEXT NOT NULL DEFAULT '';
