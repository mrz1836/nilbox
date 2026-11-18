-- 0023_checks_awaiting_on_github: the held workflow runs the user can approve only on GitHub.
--
-- The conventions are 0001_init's. The inbox owns every column here.

-- == item_details ========================================================================

-- GitHub takes the approval of a held workflow run through its API only for a fork pull
-- request's own run (the pull_request event on a pull request from another repository) or
-- one the Actions bot queued; it refuses any other, as a run a review started in the base
-- repository, which waits for approval on GitHub. From here on, checks_awaiting counts the
-- held runs the user can approve from the app, and checks_awaiting_on_github the others.
-- GitHub's rollup leaves both out, so checks_state can read SUCCESS while they wait.
--
-- A row stored before this counts every held run in checks_awaiting until the next read of
-- its details, which a whole refresh makes of every listed item.
ALTER TABLE item_details ADD COLUMN checks_awaiting_on_github INTEGER NOT NULL DEFAULT 0
    CHECK (checks_awaiting_on_github >= 0);
