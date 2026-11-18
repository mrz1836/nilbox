-- 0019_session_github_event: what came of a session's item on GitHub while it ran.
--
-- The conventions are 0001_init's. The sessions package owns every column here.

-- == agent_sessions ======================================================================

-- What the inbox learned happened on GitHub to a session's item while the session ran, so
-- the sessions panel can say so: the newest of a review of the user's that approved it or
-- requested changes (an agent's gh, a browser, the review sheet), a merge, or a close. It
-- is learned from the reads the inbox makes anyway, never a request of its own, and says
-- nothing of who did it.
--
-- github_event is approved, changes_requested, merged, or closed, '' before any;
-- github_event_commit the commit a review was left on, '' when not known or not a review;
-- github_event_at GitHub's time for it, NULL before any. A later event replaces an earlier
-- one, never the other way round.
ALTER TABLE agent_sessions ADD COLUMN github_event TEXT NOT NULL DEFAULT ''
    CHECK (github_event IN ('', 'approved', 'changes_requested', 'merged', 'closed'));
ALTER TABLE agent_sessions ADD COLUMN github_event_commit TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_sessions ADD COLUMN github_event_at DATETIME;
