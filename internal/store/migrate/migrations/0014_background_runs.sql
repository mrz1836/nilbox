-- 0014_background_runs: an agent's run in the background, recorded as a session, and
-- what it cost as an estimate.
--
-- The conventions are 0001_init's. The sessions package owns agent_sessions' column here,
-- and autopilot owns autopilot_runs'.

-- == agent_sessions ======================================================================

-- run_id is the script_runs ID of the background run an Autopilot routine started, whose
-- session this is, or 0 for a session a launch opened in the user's terminal. Such a
-- session's state comes from its run's own stream, not from an observer, so the Watcher
-- leaves it to the run; the row's pill and panel, Rereads, and LastWords follow it as they
-- follow any session.
ALTER TABLE agent_sessions ADD COLUMN run_id INTEGER NOT NULL DEFAULT 0 CHECK (run_id >= 0);

-- == autopilot_runs ======================================================================

-- cost_estimated says the run's cost is the agent's estimate of what the API would have
-- charged, as it reports for a run signed in with the user's plan rather than an API key.
ALTER TABLE autopilot_runs ADD COLUMN cost_estimated BOOLEAN NOT NULL DEFAULT 0
    CHECK (cost_estimated IN (0, 1));
