-- 0024_checks_held: the workflows of the runs GitHub holds for approval.
--
-- The conventions are 0001_init's. The inbox owns every column here.

-- == item_details ========================================================================

-- A card counts a held workflow run toward where its pull request stands only when it
-- should: a run GitHub approves only on its site doesn't count unless GitHub reports the
-- merge blocked, and a run of a workflow the user ignores (Settings, checks.ignoredWorkflows)
-- never does. The user can change that list at any time, and the card counts again at
-- once, from what is stored, without asking GitHub, so the details keep each held run.
--
-- checks_held is a JSON array of the held runs checks_awaiting and
-- checks_awaiting_on_github count, each {"workflow": its workflow's name, "" when GitHub
-- gave none, "onGitHub": whether GitHub approves it only on its site}, in GitHub's order.
-- A row stored before this holds none until the next read of its details, which a whole
-- refresh makes of every listed item; until then its counts stand for runs of no name.
ALTER TABLE item_details ADD COLUMN checks_held TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(checks_held) AND json_type(checks_held) = 'array');
